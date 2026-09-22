package biz

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	OutboxStatusPending   = "pending"
	OutboxStatusPublished = "published"
	OutboxStatusDead      = "dead"
	DefaultOutboxBatch    = 32
	DefaultOutboxLease    = 30 * time.Second
	DefaultOutboxRetries  = 3
	DefaultOutboxRetry    = 5 * time.Second
	DefaultOutboxPoll     = 1 * time.Second
)

type OutboxEvent struct {
	ID            int64
	EventID       string
	AggregateType string
	AggregateID   int64
	EventType     string
	EventVersion  int32
	Payload       json.RawMessage
	Status        string
	RetryCount    int
	NextRetryAt   *time.Time
	LeaseOwner    string
	LeaseUntil    *time.Time
	CreatedAt     time.Time
	PublishedAt   *time.Time
}

type PublishedMessage struct {
	EventID      string
	EventType    string
	EventVersion int32
	RoutingKey   string
	Body         []byte
}

type OutboxRepository interface {
	ClaimOutbox(context.Context, string, time.Time, time.Time, int) ([]OutboxEvent, error)
	MarkOutboxPublished(context.Context, int64, string, time.Time) error
	MarkOutboxFailure(context.Context, int64, string, time.Time, bool, string) error
}

type MessagePublisher interface {
	Publish(context.Context, PublishedMessage) error
}

type OutboxRelayConfig struct {
	Owner         string
	BatchSize     int
	LeaseDuration time.Duration
	MaxRetries    int
	RetryDelay    time.Duration
	PollInterval  time.Duration
}

func (c OutboxRelayConfig) normalized() OutboxRelayConfig {
	if strings.TrimSpace(c.Owner) == "" {
		c.Owner = "judge-relay"
	}
	if c.BatchSize <= 0 {
		c.BatchSize = DefaultOutboxBatch
	}
	if c.LeaseDuration <= 0 {
		c.LeaseDuration = DefaultOutboxLease
	}
	if c.MaxRetries <= 0 {
		c.MaxRetries = DefaultOutboxRetries
	}
	if c.RetryDelay <= 0 {
		c.RetryDelay = DefaultOutboxRetry
	}
	if c.PollInterval <= 0 {
		c.PollInterval = DefaultOutboxPoll
	}
	return c
}

type OutboxRelay struct {
	repository OutboxRepository
	publisher  MessagePublisher
	clock      func() time.Time
	mu         sync.RWMutex
	config     OutboxRelayConfig
}

func NewOutboxRelay(repository OutboxRepository, publisher MessagePublisher) *OutboxRelay {
	return &OutboxRelay{
		repository: repository,
		publisher:  publisher,
		clock:      func() time.Time { return time.Now().UTC() },
		config:     (OutboxRelayConfig{}).normalized(),
	}
}

func (r *OutboxRelay) Configure(config OutboxRelayConfig) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.config = config.normalized()
	r.mu.Unlock()
}

func (r *OutboxRelay) Config() OutboxRelayConfig {
	if r == nil {
		return OutboxRelayConfig{}.normalized()
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.config
}

func (r *OutboxRelay) RunOnce(ctx context.Context) (int, error) {
	if r == nil || r.repository == nil || r.publisher == nil {
		return 0, ErrorInternal("outbox relay dependencies are not configured")
	}
	config := r.Config()
	now := r.now()
	events, err := r.repository.ClaimOutbox(ctx, config.Owner, now, now.Add(config.LeaseDuration), config.BatchSize)
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, event := range events {
		message, buildErr := buildPublishedMessage(event)
		if buildErr != nil {
			if err := r.repository.MarkOutboxFailure(ctx, event.ID, config.Owner, now, true, stableFailure(buildErr)); err != nil {
				return processed, err
			}
			processed++
			continue
		}
		if err := r.publisher.Publish(ctx, message); err != nil {
			attempt := event.RetryCount + 1
			dead := attempt >= config.MaxRetries
			next := now.Add(backoff(config.RetryDelay, event.RetryCount))
			if err := r.repository.MarkOutboxFailure(ctx, event.ID, config.Owner, next, dead, stableFailure(err)); err != nil {
				return processed, err
			}
			processed++
			continue
		}
		if err := r.repository.MarkOutboxPublished(ctx, event.ID, config.Owner, now); err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}

func (r *OutboxRelay) now() time.Time {
	if r.clock == nil {
		return time.Now().UTC()
	}
	return r.clock().UTC()
}

func backoff(base time.Duration, retryCount int) time.Duration {
	if retryCount < 0 {
		retryCount = 0
	}
	if retryCount > 10 {
		retryCount = 10
	}
	for i := 0; i < retryCount; i++ {
		if base > time.Hour/2 {
			return time.Hour
		}
		base *= 2
	}
	if base > time.Hour {
		return time.Hour
	}
	return base
}

func buildPublishedMessage(event OutboxEvent) (PublishedMessage, error) {
	if strings.TrimSpace(event.EventID) == "" || event.ID <= 0 || event.AggregateID <= 0 {
		return PublishedMessage{}, fmt.Errorf("invalid outbox identity")
	}
	var payload any
	routingKey := ""
	switch event.EventType {
	case EventTypeJudgeRequested:
		var value JudgeRequestedPayload
		if err := json.Unmarshal(event.Payload, &value); err != nil || value.SubmissionID <= 0 || value.ProblemID <= 0 || !SupportedLanguage(value.Language) {
			return PublishedMessage{}, fmt.Errorf("invalid judge requested payload")
		}
		if err := ValidateJudgeRevision(value.JudgeRevision); err != nil || !validSHA256(value.SourceSHA256) || value.SourceSizeBytes <= 0 || value.SourceObjectKey == "" {
			return PublishedMessage{}, fmt.Errorf("invalid judge requested source metadata")
		}
		payload = value
		routingKey = "judge.task." + strings.ToLower(value.Language)
	case EventTypeSubmissionInvalidated:
		var value SubmissionInvalidatedPayload
		if err := json.Unmarshal(event.Payload, &value); err != nil || value.SubmissionID <= 0 || value.UserID <= 0 || value.ProblemID <= 0 || value.InvalidatedAt.IsZero() {
			return PublishedMessage{}, fmt.Errorf("invalid submission invalidated payload")
		}
		payload = value
		routingKey = EventTypeSubmissionInvalidated
	case EventTypeSubmissionJudged:
		var value SubmissionJudgedPayload
		if err := json.Unmarshal(event.Payload, &value); err != nil || value.SubmissionID <= 0 || value.UserID <= 0 || value.ProblemID <= 0 || value.JudgedAt.IsZero() {
			return PublishedMessage{}, fmt.Errorf("invalid submission judged payload")
		}
		payload = value
		routingKey = EventTypeSubmissionJudged
	default:
		return PublishedMessage{}, fmt.Errorf("unsupported outbox event type %q", event.EventType)
	}
	envelope := struct {
		EventID      string `json:"event_id"`
		EventType    string `json:"event_type"`
		EventVersion int32  `json:"event_version"`
		AggregateID  int64  `json:"aggregate_id"`
		Payload      any    `json:"payload"`
	}{event.EventID, event.EventType, event.EventVersion, event.AggregateID, payload}
	body, err := json.Marshal(envelope)
	if err != nil {
		return PublishedMessage{}, fmt.Errorf("encode outbox message: %w", err)
	}
	return PublishedMessage{EventID: event.EventID, EventType: event.EventType, EventVersion: event.EventVersion, RoutingKey: routingKey, Body: body}, nil
}

func stableFailure(err error) string {
	if err == nil {
		return "unknown_publish_failure"
	}
	message := strings.TrimSpace(err.Error())
	if len(message) > 255 {
		message = message[:255]
	}
	return message
}

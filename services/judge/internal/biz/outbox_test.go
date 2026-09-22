package biz

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestOutboxRelayPublishesAndMarksEvent(t *testing.T) {
	now := time.Date(2026, 9, 21, 3, 0, 0, 0, time.UTC)
	repository := &outboxRepositoryFake{events: []OutboxEvent{{
		ID: 1, EventID: "123e4567-e89b-12d3-a456-426614174000", AggregateID: 9,
		EventType: EventTypeJudgeRequested, EventVersion: 1, Payload: mustJSON(t, JudgeRequestedPayload{
			SubmissionID: 9, ProblemID: 7, Language: "go", JudgeRevision: "01K5C6Y7N8P9Q0R1S2T3V4W5X6",
			SourceObjectKey: "sources/a/source.go", SourceSHA256: strings.Repeat("a", 64), SourceSizeBytes: 10,
		}), RetryCount: 0,
	}}}
	publisher := &publisherFake{}
	relay := NewOutboxRelay(repository, publisher)
	relay.clock = func() time.Time { return now }
	relay.Configure(OutboxRelayConfig{Owner: "relay-1", BatchSize: 4, LeaseDuration: time.Minute})

	processed, err := relay.RunOnce(context.Background())
	if err != nil || processed != 1 {
		t.Fatalf("RunOnce() = %d, %v", processed, err)
	}
	if len(publisher.messages) != 1 || publisher.messages[0].RoutingKey != "judge.task.go" || publisher.messages[0].EventID != repository.events[0].EventID {
		t.Fatalf("published messages = %+v", publisher.messages)
	}
	if repository.publishedID != 1 || repository.publishedOwner != "relay-1" {
		t.Fatalf("published event = %d owner=%q", repository.publishedID, repository.publishedOwner)
	}
	var envelope struct {
		EventID string `json:"event_id"`
		Payload struct {
			SourceObjectKey string `json:"source_object_key"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(publisher.messages[0].Body, &envelope); err != nil || envelope.EventID == "" || envelope.Payload.SourceObjectKey == "" {
		t.Fatalf("message envelope = %s, error=%v", publisher.messages[0].Body, err)
	}
}

func TestOutboxRelayBacksOffAndDeadLettersAfterMaxRetries(t *testing.T) {
	repository := &outboxRepositoryFake{events: []OutboxEvent{{
		ID: 2, EventID: "123e4567-e89b-12d3-a456-426614174001", AggregateID: 9,
		EventType: EventTypeJudgeRequested, EventVersion: 1, Payload: mustJSON(t, JudgeRequestedPayload{
			SubmissionID: 9, ProblemID: 7, Language: "go", JudgeRevision: "01K5C6Y7N8P9Q0R1S2T3V4W5X6",
			SourceObjectKey: "sources/a/source.go", SourceSHA256: strings.Repeat("b", 64), SourceSizeBytes: 10,
		}), RetryCount: 2,
	}}}
	relay := NewOutboxRelay(repository, &publisherFake{err: errors.New("broker unavailable")})
	relay.clock = func() time.Time { return time.Date(2026, 9, 21, 3, 0, 0, 0, time.UTC) }
	relay.Configure(OutboxRelayConfig{Owner: "relay-1", MaxRetries: 3, RetryDelay: time.Second})
	if processed, err := relay.RunOnce(context.Background()); err != nil || processed != 1 {
		t.Fatalf("RunOnce() = %d, %v", processed, err)
	}
	if !repository.failureDead || repository.failureReason != "broker unavailable" {
		t.Fatalf("failure = dead=%v reason=%q", repository.failureDead, repository.failureReason)
	}
}

func TestBuildPublishedMessageRejectsUnsupportedEvents(t *testing.T) {
	if _, err := buildPublishedMessage(OutboxEvent{ID: 1, EventID: "event", AggregateID: 1, EventType: "unknown"}); err == nil {
		t.Fatal("buildPublishedMessage() accepted unsupported event")
	}
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

type outboxRepositoryFake struct {
	events         []OutboxEvent
	publishedID    int64
	publishedOwner string
	failureDead    bool
	failureReason  string
}

func (r *outboxRepositoryFake) ClaimOutbox(context.Context, string, time.Time, time.Time, int) ([]OutboxEvent, error) {
	return r.events, nil
}
func (r *outboxRepositoryFake) MarkOutboxPublished(_ context.Context, id int64, owner string, _ time.Time) error {
	r.publishedID, r.publishedOwner = id, owner
	return nil
}
func (r *outboxRepositoryFake) MarkOutboxFailure(_ context.Context, _ int64, _ string, _ time.Time, dead bool, reason string) error {
	r.failureDead, r.failureReason = dead, reason
	return nil
}

type publisherFake struct {
	messages []PublishedMessage
	err      error
}

func (p *publisherFake) Publish(_ context.Context, message PublishedMessage) error {
	if p.err != nil {
		return p.err
	}
	p.messages = append(p.messages, message)
	return nil
}

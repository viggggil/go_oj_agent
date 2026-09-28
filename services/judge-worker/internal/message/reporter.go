package message

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rabbitmq/amqp091-go"
	"github.com/viggggil/go_oj_agent/pkg/mq"
)

// Reporter publishes result events and only returns after the broker confirms them.
// The JSON envelope is the source of truth for event identity and causation.
type Reporter interface {
	ReportCompleted(context.Context, mq.Envelope, mq.JudgeCompleted) error
	ReportFailed(context.Context, mq.Envelope, mq.JudgeFailed) error
}

// RetryReporter publishes a retry task to the delayed retry queue. It is kept
// separate from Reporter so protocol-only test reporters and custom result
// reporters remain source compatible.
type RetryReporter interface {
	ReportRetry(context.Context, mq.Envelope, mq.JudgeTask) error
}

type Publisher interface {
	Publish(context.Context, string, string, []byte) error
}

// PublisherReporter is the protocol-only reporter used by tests and by
// deployments that provide their own confirmed publisher.
type PublisherReporter struct{ Publisher Publisher }

func NewReporter(p Publisher) *PublisherReporter { return &PublisherReporter{Publisher: p} }

func (r *PublisherReporter) ReportCompleted(ctx context.Context, input mq.Envelope, result mq.JudgeCompleted) error {
	if err := result.Validate(); err != nil {
		return err
	}
	return r.report(ctx, input, mq.EventTypeJudgeCompleted, mq.RoutingJudgeCompleted, result)
}

func (r *PublisherReporter) ReportFailed(ctx context.Context, input mq.Envelope, result mq.JudgeFailed) error {
	if err := result.Validate(); err != nil {
		return err
	}
	return r.report(ctx, input, mq.EventTypeJudgeFailed, mq.RoutingJudgeFailed, result)
}

func (r *PublisherReporter) ReportRetry(ctx context.Context, input mq.Envelope, task mq.JudgeTask) error {
	if err := task.Validate(); err != nil {
		return err
	}
	return r.reportTask(ctx, input, task)
}

func (r *PublisherReporter) report(ctx context.Context, input mq.Envelope, eventType, route string, payload any) error {
	if r == nil || r.Publisher == nil {
		return fmt.Errorf("publisher reporter is not configured")
	}
	body, err := marshalResult(input, eventType, payload)
	if err != nil {
		return err
	}
	var envelope mq.Envelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return err
	}
	return r.Publisher.Publish(ctx, route, envelope.EventID, body)
}

func (r *PublisherReporter) reportTask(ctx context.Context, input mq.Envelope, task mq.JudgeTask) error {
	if r == nil || r.Publisher == nil {
		return fmt.Errorf("publisher reporter is not configured")
	}
	body, err := marshalTask(input, task)
	if err != nil {
		return err
	}
	var envelope mq.Envelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return err
	}
	return r.Publisher.Publish(ctx, retryRoute(task.Language), envelope.EventID, body)
}

type RabbitReporter struct {
	url, exchange  string
	confirmTimeout time.Duration
	mu             sync.Mutex
	conn           *amqp091.Connection
	channel        *amqp091.Channel
}

func NewRabbitReporter(url, exchange string, confirmTimeout time.Duration) *RabbitReporter {
	if confirmTimeout <= 0 {
		confirmTimeout = 5 * time.Second
	}
	return &RabbitReporter{url: url, exchange: exchange, confirmTimeout: confirmTimeout}
}

func (r *RabbitReporter) ReportCompleted(ctx context.Context, input mq.Envelope, result mq.JudgeCompleted) error {
	if err := result.Validate(); err != nil {
		return err
	}
	return r.report(ctx, input, mq.EventTypeJudgeCompleted, mq.RoutingJudgeCompleted, result)
}

func (r *RabbitReporter) ReportFailed(ctx context.Context, input mq.Envelope, result mq.JudgeFailed) error {
	if err := result.Validate(); err != nil {
		return err
	}
	return r.report(ctx, input, mq.EventTypeJudgeFailed, mq.RoutingJudgeFailed, result)
}

func (r *RabbitReporter) ReportRetry(ctx context.Context, input mq.Envelope, task mq.JudgeTask) error {
	if err := task.Validate(); err != nil {
		return err
	}
	return r.reportTask(ctx, input, task)
}

func (r *RabbitReporter) report(ctx context.Context, input mq.Envelope, eventType, route string, payload any) error {
	if r == nil || r.url == "" || r.exchange == "" {
		return fmt.Errorf("rabbit reporter is not configured")
	}
	body, err := marshalResult(input, eventType, payload)
	if err != nil {
		return err
	}
	var envelope mq.Envelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return err
	}
	return r.publish(ctx, route, envelope.EventID, body)
}

func (r *RabbitReporter) reportTask(ctx context.Context, input mq.Envelope, task mq.JudgeTask) error {
	if r == nil || r.url == "" || r.exchange == "" {
		return fmt.Errorf("rabbit reporter is not configured")
	}
	body, err := marshalTask(input, task)
	if err != nil {
		return err
	}
	var envelope mq.Envelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return err
	}
	return r.publish(ctx, retryRoute(task.Language), envelope.EventID, body)
}

func marshalResult(input mq.Envelope, eventType string, payload any) ([]byte, error) {
	return mq.MarshalEnvelope(mq.EnvelopeMetadata{EventID: uuid.NewString(), EventType: eventType, EventVersion: mq.EventVersion1, OccurredAt: time.Now().UTC(), TraceID: input.TraceID, CausationID: input.EventID}, payload)
}

func marshalTask(input mq.Envelope, task mq.JudgeTask) ([]byte, error) {
	return mq.MarshalEnvelope(mq.EnvelopeMetadata{
		EventID: uuid.NewString(), EventType: mq.EventTypeJudgeTask, EventVersion: mq.EventVersion1,
		OccurredAt: time.Now().UTC(), TraceID: input.TraceID, CausationID: input.EventID,
	}, task)
}

func retryRoute(language string) string {
	// Retry queues are language-specific so a delayed message is routed back to
	// the same worker queue when the TTL expires.
	switch language {
	case "go":
		return "judge.retry.go"
	case "cpp":
		return "judge.retry.cpp"
	case "python":
		return "judge.retry.python"
	case "java":
		return "judge.retry.java"
	default:
		return "judge.retry.go"
	}
}

func (r *RabbitReporter) publish(ctx context.Context, route, messageID string, body []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	pctx, cancel := context.WithTimeout(ctx, r.confirmTimeout)
	defer cancel()
	if err := r.ensureConnectedLocked(); err != nil {
		return err
	}
	confirm, err := r.channel.PublishWithDeferredConfirmWithContext(pctx, r.exchange, route, false, false, amqp091.Publishing{ContentType: "application/json", DeliveryMode: amqp091.Persistent, MessageId: messageID, Type: route, Body: body, Timestamp: time.Now().UTC()})
	if err != nil {
		r.resetLocked()
		return err
	}
	if confirm == nil {
		r.resetLocked()
		return fmt.Errorf("publisher confirm unavailable")
	}
	ok, err := confirm.WaitContext(pctx)
	if err != nil {
		r.resetLocked()
		return err
	}
	if !ok {
		r.resetLocked()
		return fmt.Errorf("publisher confirm rejected")
	}
	return nil
}

func (r *RabbitReporter) ensureConnectedLocked() error {
	if r.conn != nil && !r.conn.IsClosed() && r.channel != nil {
		return nil
	}
	if err := r.resetLocked(); err != nil {
		return err
	}
	conn, err := amqp091.Dial(r.url)
	if err != nil {
		return err
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return err
	}
	if err = ch.Confirm(false); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return err
	}
	if err = ch.ExchangeDeclare(r.exchange, amqp091.ExchangeTopic, true, false, false, false, nil); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return err
	}
	r.conn, r.channel = conn, ch
	return nil
}

func (r *RabbitReporter) resetLocked() error {
	if r.channel != nil {
		_ = r.channel.Close()
	}
	if r.conn != nil {
		_ = r.conn.Close()
	}
	r.channel, r.conn = nil, nil
	return nil
}

func (r *RabbitReporter) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.resetLocked()
}

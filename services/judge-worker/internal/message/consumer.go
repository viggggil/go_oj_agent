package message

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/rabbitmq/amqp091-go"
	"github.com/viggggil/go_oj_agent/pkg/mq"
	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/biz"
)

type Delivery interface {
	GetBody() []byte
	Ack(bool) error
	Nack(bool, bool) error
	Reject(bool) error
}

type delivery struct{ amqp091.Delivery }

func (d delivery) GetBody() []byte      { return d.Body }
func (d delivery) GetMessageID() string { return d.MessageId }

type Engine interface {
	Execute(context.Context, mq.JudgeTask) biz.Outcome
}

type Consumer struct {
	URL, Exchange, Queue string
	Concurrency          int
	TaskTimeout          time.Duration
	Engine               Engine
	Reporter             Reporter
	Logger               *slog.Logger
	conn                 *amqp091.Connection
	channel              *amqp091.Channel
	closeOnce            sync.Once
}

func (c *Consumer) Handle(ctx context.Context, d Delivery) {
	if c == nil || d == nil {
		return
	}
	envelope, task, err := decodeTask(d.GetBody())
	if err != nil {
		if c.Logger != nil {
			attrs := []any{"reason", err.Error()}
			if metadata, ok := d.(interface{ GetMessageID() string }); ok {
				attrs = append(attrs, "message_id", metadata.GetMessageID())
			}
			c.Logger.Warn("task rejected", attrs...)
		}
		_ = d.Reject(false)
		return
	}
	if c.Logger != nil {
		c.Logger.Info("task received", "event_id", envelope.EventID, "submission_id", task.SubmissionID, "problem_id", task.ProblemID)
	}
	deadline := time.Now().Add(c.TaskTimeout)
	if c.TaskTimeout <= 0 {
		deadline = time.Now().Add(60 * time.Second)
	}
	if task.JudgeDeadlineAt.Before(deadline) {
		deadline = task.JudgeDeadlineAt
	}
	taskCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	if c.Logger != nil {
		c.Logger.Info("judge started", "event_id", envelope.EventID, "submission_id", task.SubmissionID)
	}
	if c.Engine == nil || c.Reporter == nil {
		_ = d.Nack(false, true)
		return
	}
	outcome := c.Engine.Execute(taskCtx, task)
	if outcome.Completed != nil {
		if c.Logger != nil {
			c.Logger.Info("judge completed", "event_id", envelope.EventID, "submission_id", task.SubmissionID, "verdict", outcome.Completed.Verdict)
		}
		// The execution deadline must not cancel publication of the resulting
		// verdict; otherwise an expired task could be requeued forever.
		err = c.Reporter.ReportCompleted(ctx, envelope, *outcome.Completed)
	} else if outcome.Failed != nil {
		if c.Logger != nil {
			c.Logger.Warn("judge failed", "event_id", envelope.EventID, "submission_id", task.SubmissionID, "reason", outcome.Failed.Reason)
		}
		err = c.Reporter.ReportFailed(ctx, envelope, *outcome.Failed)
	} else {
		err = fmt.Errorf("engine returned empty outcome")
	}
	if err != nil {
		if c.Logger != nil {
			c.Logger.Error("result publish failed", "event_id", envelope.EventID, "error", err)
		}
		_ = d.Nack(false, true)
		return
	}
	if c.Logger != nil {
		c.Logger.Info("result publish confirmed", "event_id", envelope.EventID)
	}
	if err = d.Ack(false); err != nil && c.Logger != nil {
		c.Logger.Error("task ack failed", "event_id", envelope.EventID, "error", err)
	}
}

func decodeTask(body []byte) (mq.Envelope, mq.JudgeTask, error) {
	var task mq.JudgeTask
	envelope, err := mq.UnmarshalEnvelope(body, mq.EventTypeJudgeTask, &task)
	if err != nil {
		return mq.Envelope{}, task, err
	}
	if task.Language != "go" {
		return envelope, task, fmt.Errorf("unsupported language %q", task.Language)
	}
	if err = task.Validate(); err != nil {
		return envelope, task, err
	}
	return envelope, task, nil
}

// DecodeTask validates the full event envelope and the Go worker task contract.
func DecodeTask(body []byte) (mq.Envelope, mq.JudgeTask, error) { return decodeTask(body) }

func (c *Consumer) Start(ctx context.Context) error {
	if c == nil || c.Engine == nil || c.Reporter == nil {
		return fmt.Errorf("consumer dependencies are not configured")
	}
	if c.URL == "" || c.Exchange == "" {
		return fmt.Errorf("rabbitmq url and exchange are required")
	}
	if c.Queue == "" {
		c.Queue = mq.RoutingJudgeTaskGo
	}
	if c.Concurrency < 1 {
		return fmt.Errorf("consumer concurrency must be positive")
	}
	conn, err := amqp091.Dial(c.URL)
	if err != nil {
		return err
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return err
	}
	if err = declareTopology(ch, c.Exchange, c.Queue); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return err
	}
	if err = ch.Qos(c.Concurrency, 0, false); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return err
	}
	deliveries, err := ch.Consume(c.Queue, "", false, false, false, false, nil)
	if err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return err
	}
	c.conn, c.channel = conn, ch
	workers := c.Concurrency
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case d, ok := <-deliveries:
					if !ok {
						return
					}
					c.Handle(ctx, delivery{d})
				}
			}
		}()
	}
	<-ctx.Done()
	_ = ch.Close()
	_ = conn.Close()
	wg.Wait()
	return nil
}

func declareTopology(ch *amqp091.Channel, exchange, queue string) error {
	if err := ch.ExchangeDeclare(exchange, amqp091.ExchangeTopic, true, false, false, false, nil); err != nil {
		return err
	}
	if _, err := ch.QueueDeclare(queue, true, false, false, false, nil); err != nil {
		return err
	}
	return ch.QueueBind(queue, mq.RoutingJudgeTaskGo, exchange, false, nil)
}

func (c *Consumer) Close() error {
	if c == nil {
		return nil
	}
	c.closeOnce.Do(func() {
		if c.channel != nil {
			_ = c.channel.Close()
		}
		if c.conn != nil {
			_ = c.conn.Close()
		}
		if closer, ok := c.Reporter.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	})
	return nil
}

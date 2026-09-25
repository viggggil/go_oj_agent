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
	RetryQueue, DLQ      string
	Concurrency          int
	TaskTimeout          time.Duration
	ShutdownTimeout      time.Duration
	MaxRetries           int
	RetryDelay           time.Duration
	Engine               Engine
	Reporter             Reporter
	Logger               *slog.Logger
	conn                 *amqp091.Connection
	channel              *amqp091.Channel
	consumerTag          string
	stop                 context.CancelFunc
	done                 chan struct{}
	started              bool
	mu                   sync.Mutex
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
	// Expired tasks are terminal and must never reach the sandbox. This check
	// remains here in addition to Engine's guard for protocol-only engines.
	if !time.Now().Before(task.JudgeDeadlineAt) {
		failed := mq.JudgeFailed{SubmissionID: task.SubmissionID, JudgeRevision: task.JudgeRevision, Code: mq.FailureTaskExpired, Message: "judge deadline exceeded", Reason: "JUDGE_DEADLINE_EXCEEDED", Retryable: false}
		if err := c.Reporter.ReportFailed(ctx, envelope, failed); err != nil {
			_ = d.Nack(false, true)
			return
		}
		_ = d.Ack(false)
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
		// Retry is an explicit publish to a delayed queue. Never NACK/requeue a
		// retryable engine result directly, otherwise a busy loop can starve the
		// worker and defeat the retry counter.
		if outcome.Failed.Retryable && c.retryAllowed(task) {
			retryTask := task
			retryTask.Attempt++
			if retryReporter, ok := c.Reporter.(RetryReporter); ok {
				err = retryReporter.ReportRetry(ctx, envelope, retryTask)
				if err == nil {
					err = d.Ack(false)
				}
				if err != nil {
					_ = d.Nack(false, true)
				}
				return
			}
			_ = d.Nack(false, true)
			return
		}
		// Retry budget/deadline exhausted: emit a terminal failure while
		// preserving the structured diagnostic code and message.
		outcome.Failed.Retryable = false
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

func (c *Consumer) retryAllowed(task mq.JudgeTask) bool {
	maxRetries := int32(c.MaxRetries)
	if maxRetries < 0 {
		maxRetries = 0
	}
	if task.Attempt >= maxRetries {
		return false
	}
	delay := c.RetryDelay
	if delay <= 0 {
		delay = 5 * time.Second
	}
	return time.Now().Add(delay).Before(task.JudgeDeadlineAt)
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
	if err = declareTopology(ch, c.Exchange, c.Queue, c.RetryQueue, c.DLQ, c.RetryDelay); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return err
	}
	if err = ch.Qos(c.Concurrency, 0, false); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return err
	}
	consumerTag := "judge-worker"
	deliveries, err := ch.Consume(c.Queue, consumerTag, false, false, false, false, nil)
	if err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return err
	}
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	c.mu.Lock()
	c.conn, c.channel, c.consumerTag = conn, ch, consumerTag
	c.stop, c.done, c.started = stop, done, true
	c.mu.Unlock()
	defer func() {
		_ = c.closeReporter()
		c.mu.Lock()
		c.conn, c.channel, c.stop, c.done, c.consumerTag, c.started = nil, nil, nil, nil, "", false
		close(done)
		c.mu.Unlock()
	}()
	workers := c.Concurrency
	processingCtx := context.WithoutCancel(runCtx)
	workersDone := runDeliveryWorkers(deliveries, workers, func(d amqp091.Delivery) {
		c.Handle(processingCtx, delivery{d})
	})
	var runErr error
	select {
	case <-runCtx.Done():
		// Cancel tells RabbitMQ to stop delivering new messages. The delivery
		// channel is drained by the workers before any AMQP resource closes.
		_ = ch.Cancel(consumerTag, false)
		waitCtx := context.Background()
		if c.ShutdownTimeout > 0 {
			var cancel context.CancelFunc
			waitCtx, cancel = context.WithTimeout(waitCtx, c.ShutdownTimeout)
			defer cancel()
		}
		select {
		case <-workersDone:
		case <-waitCtx.Done():
			// Closing the channel below leaves unfinished deliveries unacked so
			// RabbitMQ can redeliver them after reconnect.
		}
	case <-workersDone:
		if ctx.Err() == nil {
			runErr = fmt.Errorf("rabbitmq consumer delivery channel closed")
		}
	}
	// All in-flight deliveries have completed before AMQP resources close.
	_ = ch.Close()
	_ = conn.Close()
	return runErr
}

// runDeliveryWorkers drains the broker delivery channel before it reports
// completion. Shutdown cancels the broker consumer first, then waits on this
// channel before closing the AMQP channel and connection.
func runDeliveryWorkers(deliveries <-chan amqp091.Delivery, workers int, handle func(amqp091.Delivery)) <-chan struct{} {
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for d := range deliveries {
				handle(d)
			}
		}()
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	return done
}

func declareTopology(ch *amqp091.Channel, exchange, queue, retryQueue, dlq string, retryDelay time.Duration) error {
	if err := ch.ExchangeDeclare(exchange, amqp091.ExchangeTopic, true, false, false, false, nil); err != nil {
		return err
	}
	if dlq == "" {
		dlq = "judge.dlq"
	}
	if retryQueue == "" {
		retryQueue = "judge.retry.go"
	}
	if _, err := ch.QueueDeclare(dlq, true, false, false, false, nil); err != nil {
		return err
	}
	if err := ch.QueueBind(dlq, dlq, exchange, false, nil); err != nil {
		return err
	}
	if _, err := ch.QueueDeclare(queue, true, false, false, false, amqp091.Table{
		"x-dead-letter-exchange":    exchange,
		"x-dead-letter-routing-key": dlq,
	}); err != nil {
		return err
	}
	if err := ch.QueueBind(queue, mq.RoutingJudgeTaskGo, exchange, false, nil); err != nil {
		return err
	}
	// The retry queue has no consumer. Messages are held for a fixed delay and
	// dead-lettered back to the original task routing key.
	if retryDelay <= 0 {
		retryDelay = 5 * time.Second
	}
	if _, err := ch.QueueDeclare(retryQueue, true, false, false, false, amqp091.Table{
		"x-message-ttl":             int32(retryDelay / time.Millisecond),
		"x-dead-letter-exchange":    exchange,
		"x-dead-letter-routing-key": mq.RoutingJudgeTaskGo,
	}); err != nil {
		return err
	}
	return ch.QueueBind(retryQueue, "judge.retry.go", exchange, false, nil)
}

func (c *Consumer) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	stop, done, started := c.stop, c.done, c.started
	c.mu.Unlock()
	if started && stop != nil {
		stop()
		<-done
		return nil
	}
	return c.closeReporter()
}

func (c *Consumer) closeReporter() error {
	if closer, ok := c.Reporter.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}

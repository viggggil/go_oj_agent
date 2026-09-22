package message

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rabbitmq/amqp091-go"

	"github.com/viggggil/go_oj_agent/services/judge/internal/biz"
	"github.com/viggggil/go_oj_agent/services/judge/internal/conf"
)

type RabbitPublisher struct {
	url      string
	exchange string
	mu       sync.Mutex
	conn     *amqp091.Connection
	channel  *amqp091.Channel
}

type ResultHandler interface {
	Handle(context.Context, string, []byte) error
}

type RabbitResultConsumer struct {
	url, exchange, queue string
	handler              ResultHandler
	conn                 *amqp091.Connection
	channel              *amqp091.Channel
	mu                   sync.Mutex
	cancel               context.CancelFunc
}

var judgeEventRoutes = []string{"judge.task.cpp", "judge.task.go", "judge.task.python", "judge.task.java", biz.EventTypeSubmissionInvalidated, biz.EventTypeSubmissionJudged}

func NewRabbitResultConsumer(config *conf.Bootstrap, handler ResultHandler) (*RabbitResultConsumer, func(), error) {
	if config == nil || config.GetMessaging() == nil || config.GetMessaging().GetRabbitmq() == nil {
		return nil, func() {}, fmt.Errorf("judge rabbitmq configuration is required")
	}
	rabbit := config.GetMessaging().GetRabbitmq()
	if rabbit.GetUrl() == "" || rabbit.GetExchange() == "" || rabbit.GetResultQueue() == "" {
		return nil, func() {}, fmt.Errorf("judge rabbitmq result consumer configuration is required")
	}
	c := &RabbitResultConsumer{url: rabbit.GetUrl(), exchange: rabbit.GetExchange(), queue: rabbit.GetResultQueue(), handler: handler}
	return c, func() { _ = c.Close() }, nil
}

func (c *RabbitResultConsumer) Start(ctx context.Context) error {
	if c == nil || c.handler == nil {
		return fmt.Errorf("rabbitmq result consumer is not configured")
	}
	conn, err := amqp091.Dial(c.url)
	if err != nil {
		return err
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return err
	}
	if err = ch.ExchangeDeclare(c.exchange, amqp091.ExchangeTopic, true, false, false, false, nil); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return err
	}
	if _, err = ch.QueueDeclare(c.queue, true, false, false, false, nil); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return err
	}
	for _, route := range judgeEventRoutes {
		if _, err = ch.QueueDeclare(route, true, false, false, false, nil); err != nil {
			_ = ch.Close()
			_ = conn.Close()
			return err
		}
		if err = ch.QueueBind(route, route, c.exchange, false, nil); err != nil {
			_ = ch.Close()
			_ = conn.Close()
			return err
		}
	}
	for _, key := range []string{EventRoutingJudgeCompleted, EventRoutingJudgeFailed} {
		if err = ch.QueueBind(c.queue, key, c.exchange, false, nil); err != nil {
			_ = ch.Close()
			_ = conn.Close()
			return err
		}
	}
	if err = ch.Qos(16, 0, false); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return err
	}
	deliveries, err := ch.Consume(c.queue, "judge-result-consumer", false, false, false, false, nil)
	if err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return err
	}
	runCtx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	c.conn, c.channel, c.cancel = conn, ch, cancel
	c.mu.Unlock()
	defer c.Close()
	for {
		select {
		case <-runCtx.Done():
			return nil
		case delivery, ok := <-deliveries:
			if !ok {
				if runCtx.Err() != nil {
					return nil
				}
				return fmt.Errorf("rabbitmq result consumer channel closed")
			}
			err := c.handler.Handle(runCtx, delivery.Type, delivery.Body)
			if err == nil {
				_ = delivery.Ack(false)
			} else if biz.HasReason(err, biz.ReasonInvalidArgument) {
				_ = delivery.Reject(false)
			} else {
				_ = delivery.Nack(false, true)
			}
		}
	}
}

func (c *RabbitResultConsumer) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	if c.channel != nil {
		_ = c.channel.Close()
		c.channel = nil
	}
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
	return nil
}

const (
	EventRoutingJudgeCompleted = "judge.completed"
	EventRoutingJudgeFailed    = "judge.failed"
)

func NewRabbitPublisher(config *conf.Bootstrap) (*RabbitPublisher, func(), error) {
	if config == nil || config.GetMessaging() == nil || config.GetMessaging().GetRabbitmq() == nil {
		return nil, func() {}, fmt.Errorf("judge rabbitmq configuration is required")
	}
	rabbit := config.GetMessaging().GetRabbitmq()
	if rabbit.GetUrl() == "" || rabbit.GetExchange() == "" {
		return nil, func() {}, fmt.Errorf("judge rabbitmq url and exchange are required")
	}
	publisher := &RabbitPublisher{url: rabbit.GetUrl(), exchange: rabbit.GetExchange()}
	return publisher, func() { _ = publisher.Close() }, nil
}

func (p *RabbitPublisher) Publish(ctx context.Context, message biz.PublishedMessage) error {
	if p == nil {
		return fmt.Errorf("rabbitmq publisher is not configured")
	}
	if message.EventID == "" || message.RoutingKey == "" || len(message.Body) == 0 {
		return fmt.Errorf("invalid message envelope")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.ensureConnectedLocked(); err != nil {
		return err
	}
	confirm, err := p.channel.PublishWithDeferredConfirmWithContext(ctx, p.exchange, message.RoutingKey, false, false, amqp091.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp091.Persistent,
		MessageId:    message.EventID,
		Type:         message.EventType,
		Timestamp:    time.Now().UTC(),
		Body:         append([]byte(nil), message.Body...),
	})
	if err != nil {
		p.resetLocked()
		return err
	}
	if confirm == nil {
		p.resetLocked()
		return fmt.Errorf("rabbitmq publisher confirm was not created")
	}
	confirmed, err := confirm.WaitContext(ctx)
	if err != nil {
		p.resetLocked()
		return err
	}
	if !confirmed {
		p.resetLocked()
		return fmt.Errorf("rabbitmq publisher confirm was rejected")
	}
	return nil
}

func (p *RabbitPublisher) connectLocked() error {
	conn, err := amqp091.Dial(p.url)
	if err != nil {
		return err
	}
	channel, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return err
	}
	if err = channel.Confirm(false); err != nil {
		_ = channel.Close()
		_ = conn.Close()
		return err
	}
	if err = channel.ExchangeDeclare(p.exchange, amqp091.ExchangeTopic, true, false, false, false, nil); err != nil {
		_ = channel.Close()
		_ = conn.Close()
		return err
	}
	for _, route := range judgeEventRoutes {
		if _, err = channel.QueueDeclare(route, true, false, false, false, nil); err != nil {
			_ = channel.Close()
			_ = conn.Close()
			return err
		}
		if err = channel.QueueBind(route, route, p.exchange, false, nil); err != nil {
			_ = channel.Close()
			_ = conn.Close()
			return err
		}
	}
	p.conn = conn
	p.channel = channel
	return nil
}

func (p *RabbitPublisher) ensureConnectedLocked() error {
	if p.conn != nil && !p.conn.IsClosed() && p.channel != nil {
		return nil
	}
	p.resetLocked()
	return p.connectLocked()
}

func (p *RabbitPublisher) resetLocked() {
	if p.channel != nil {
		_ = p.channel.Close()
	}
	if p.conn != nil {
		_ = p.conn.Close()
	}
	p.channel = nil
	p.conn = nil
}

func (p *RabbitPublisher) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.resetLocked()
	return nil
}

var _ biz.MessagePublisher = (*RabbitPublisher)(nil)

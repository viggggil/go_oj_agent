package server

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/viggggil/go_oj_agent/pkg/mq"
	"github.com/viggggil/go_oj_agent/services/contest/internal/biz"
	"github.com/viggggil/go_oj_agent/services/contest/internal/conf"
	"github.com/viggggil/go_oj_agent/services/contest/internal/data"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ResultConsumerServer struct {
	config  *conf.MessagingProto
	handler *biz.ProjectionConsumer
	mu      sync.Mutex
	cancel  context.CancelFunc
	done    chan struct{}
}

func NewResultConsumerServer(c *conf.Bootstrap, r *data.Repository) (*ResultConsumerServer, error) {
	if c.GetMessaging() == nil {
		return nil, fmt.Errorf("contest messaging configuration is required")
	}
	return &ResultConsumerServer{config: c.GetMessaging(), handler: biz.NewProjectionConsumer(r)}, nil
}

func (s *ResultConsumerServer) connect() (*amqp.Connection, *amqp.Channel, error) {
	conn, err := amqp.DialConfig(s.config.GetUrl(), amqp.Config{Dial: amqp.DefaultDial(5 * time.Second)})
	if err != nil {
		return nil, nil, err
	}
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	if err = ch.ExchangeDeclare(s.config.GetExchange(), amqp.ExchangeTopic, true, false, false, false, nil); err == nil {
		_, err = ch.QueueDeclare(s.config.GetDeadLetterQueue(), true, false, false, false, nil)
	}
	if err == nil {
		err = ch.QueueBind(s.config.GetDeadLetterQueue(), s.config.GetDeadLetterQueue(), s.config.GetExchange(), false, nil)
	}
	if err == nil {
		_, err = ch.QueueDeclare(s.config.GetQueue(), true, false, false, false, amqp.Table{"x-dead-letter-exchange": s.config.GetExchange(), "x-dead-letter-routing-key": s.config.GetDeadLetterQueue()})
	}
	for _, route := range []string{mq.RoutingSubmissionJudged, mq.RoutingSubmissionInvalidated} {
		if err == nil {
			err = ch.QueueBind(s.config.GetQueue(), route, s.config.GetExchange(), false, nil)
		}
	}
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, nil, err
	}
	return conn, ch, nil
}

func (s *ResultConsumerServer) Prepare(context.Context) error {
	conn, ch, err := s.connect()
	if err != nil {
		return err
	}
	defer conn.Close()
	defer ch.Close()
	return nil
}

func (s *ResultConsumerServer) Start(ctx context.Context) error {
	run, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	if s.done != nil {
		s.mu.Unlock()
		cancel()
		return fmt.Errorf("contest consumer already started")
	}
	s.cancel, s.done = cancel, make(chan struct{})
	done := s.done
	s.mu.Unlock()
	defer close(done)
	defer cancel()
	conn, ch, err := s.connect()
	if err != nil {
		return err
	}
	defer conn.Close()
	defer ch.Close()
	if err := ch.Qos(int(s.config.GetPrefetch()), 0, false); err != nil {
		return err
	}
	deliveries, err := ch.Consume(s.config.GetQueue(), "", false, false, false, false, nil)
	if err != nil {
		return err
	}
	for {
		select {
		case <-run.Done():
			return nil
		case d, ok := <-deliveries:
			if !ok {
				return fmt.Errorf("contest consumer channel closed")
			}
			envelope, parseErr := mq.UnmarshalEnvelope(d.Body, d.Type, &map[string]any{})
			if parseErr == nil && d.RoutingKey != envelope.EventType {
				parseErr = fmt.Errorf("contest event routing mismatch")
			}
			if parseErr != nil {
				slog.Warn("contest event rejected", "message_id", d.MessageId, "error", parseErr)
				err = d.Reject(false)
			} else {
				handleCtx, cancelHandle := context.WithTimeout(run, 10*time.Second)
				err = s.handler.Handle(handleCtx, d.Body)
				cancelHandle()
				switch {
				case err == nil:
					err = d.Ack(false)
				case status.Code(err) == codes.InvalidArgument:
					slog.Warn("contest event rejected", "event_id", envelope.EventID, "error", err)
					err = d.Reject(false)
				default:
					slog.Warn("contest event requeued", "event_id", envelope.EventID, "error", err)
					err = d.Nack(false, true)
					// 避免持久业务错误造成无界热循环，停止期间仍可及时退出。
					timer := time.NewTimer(100 * time.Millisecond)
					select {
					case <-run.Done():
						timer.Stop()
						return nil
					case <-timer.C:
					}
				}
			}
			if err != nil {
				return err
			}
		}
	}
}

func (s *ResultConsumerServer) Stop(ctx context.Context) error {
	s.mu.Lock()
	cancel, done := s.cancel, s.done
	s.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

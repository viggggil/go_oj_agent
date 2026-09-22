package server

import (
	"context"
	"fmt"
	"sync"

	"github.com/go-kratos/kratos/v3/transport"
	"github.com/viggggil/go_oj_agent/services/judge/internal/message"
)

type ResultConsumerServer struct {
	consumer *message.RabbitResultConsumer
	mu       sync.Mutex
	done     chan struct{}
}

func NewResultConsumerServer(consumer *message.RabbitResultConsumer) *ResultConsumerServer {
	return &ResultConsumerServer{consumer: consumer}
}

func (s *ResultConsumerServer) Start(ctx context.Context) error {
	if s == nil || s.consumer == nil {
		return fmt.Errorf("judge result consumer server is not configured")
	}
	s.mu.Lock()
	if s.done != nil {
		s.mu.Unlock()
		return fmt.Errorf("judge result consumer server already started")
	}
	s.done = make(chan struct{})
	done := s.done
	s.mu.Unlock()
	defer close(done)
	return s.consumer.Start(ctx)
}

func (s *ResultConsumerServer) Stop(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.consumer.Close()
	s.mu.Lock()
	done := s.done
	s.mu.Unlock()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

var _ transport.Server = (*ResultConsumerServer)(nil)

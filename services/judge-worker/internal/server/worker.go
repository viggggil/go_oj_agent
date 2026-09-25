package server

import (
	"context"
	"fmt"
	"sync"

	"github.com/go-kratos/kratos/v3/transport"

	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/biz"
	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/message"
	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/sandbox"
)

type Worker struct {
	sandbox  sandbox.Executor
	engine   *biz.Engine
	consumer *message.Consumer
	mu       sync.Mutex
	done     chan struct{}
}

func NewWorker(executor sandbox.Executor, engine *biz.Engine, consumers ...*message.Consumer) *Worker {
	var consumer *message.Consumer
	if len(consumers) > 0 {
		consumer = consumers[0]
	}
	return &Worker{sandbox: executor, engine: engine, consumer: consumer}
}

func NewWorkerWithConsumer(executor sandbox.Executor, engine *biz.Engine, consumer *message.Consumer) *Worker {
	return &Worker{sandbox: executor, engine: engine, consumer: consumer}
}

func (w *Worker) Start(ctx context.Context) error {
	if w == nil || w.sandbox == nil || w.engine == nil {
		return fmt.Errorf("judge-worker dependencies are not configured")
	}
	w.mu.Lock()
	if w.done != nil {
		w.mu.Unlock()
		return fmt.Errorf("judge-worker already started")
	}
	w.done = make(chan struct{})
	done := w.done
	w.mu.Unlock()
	defer close(done)
	if w.consumer != nil {
		consumerErr := make(chan error, 1)
		go func() { consumerErr <- w.consumer.Start(ctx) }()
		select {
		case err := <-consumerErr:
			if err != nil && ctx.Err() == nil {
				return err
			}
		case <-ctx.Done():
			_ = w.consumer.Close()
			return nil
		}
	}
	<-ctx.Done()
	return nil
}

func (w *Worker) Stop(ctx context.Context) error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	done := w.done
	w.mu.Unlock()
	if done == nil {
		return nil
	}
	if w.consumer != nil {
		_ = w.consumer.Close()
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

var _ transport.Server = (*Worker)(nil)

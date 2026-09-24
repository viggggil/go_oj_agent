package server

import (
	"context"
	"fmt"
	"sync"

	"github.com/go-kratos/kratos/v3/transport"

	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/biz"
	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/sandbox"
)

// Worker owns the process lifecycle. RabbitMQ delivery is intentionally added
// in the next issue; this server keeps the new deployment unit lifecycle-safe.
type Worker struct {
	sandbox sandbox.Executor
	engine  *biz.Engine
	mu      sync.Mutex
	done    chan struct{}
}

func NewWorker(executor sandbox.Executor, engine *biz.Engine) *Worker {
	return &Worker{sandbox: executor, engine: engine}
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
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

var _ transport.Server = (*Worker)(nil)

package server

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-kratos/kratos/v3/transport"

	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/biz"
	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/conf"
	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/message"
	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/sandbox"
)

type RunCodeOptions struct {
	Address string
	Token   string
	Runners map[string]biz.LanguageRunner
}

type Worker struct {
	sandbox  sandbox.Executor
	engine   *biz.Engine
	consumer *message.Consumer
	runCode  *http.Server
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

func NewWorkerWithConsumer(executor sandbox.Executor, engine *biz.Engine, consumer *message.Consumer, options ...RunCodeOptions) *Worker {
	worker := &Worker{sandbox: executor, engine: engine, consumer: consumer}
	if len(options) > 0 {
		option := options[0]
		worker.runCode = &http.Server{
			Addr: option.Address, Handler: NewRunCodeHandler(option.Runners, option.Token),
			ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second,
			WriteTimeout: 75 * time.Second, IdleTimeout: 15 * time.Second,
			MaxHeaderBytes: 8 << 10,
		}
	}
	return worker
}

func NewWorkerWithRunCode(executor sandbox.Executor, engine *biz.Engine, consumer *message.Consumer, config *conf.Bootstrap, runners map[string]biz.LanguageRunner) *Worker {
	return NewWorkerWithConsumer(executor, engine, consumer, RunCodeOptions{
		Address: config.RunCode.Address,
		Token:   config.RunCode.AuthToken,
		Runners: runners,
	})
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
	var httpErrors chan error
	if w.runCode != nil {
		httpErrors = make(chan error, 1)
		go func() { httpErrors <- w.runCode.ListenAndServe() }()
	}
	if w.consumer != nil {
		consumerErr := make(chan error, 1)
		go func() { consumerErr <- w.consumer.Start(ctx) }()
		select {
		case err := <-consumerErr:
			if err != nil && ctx.Err() == nil {
				return err
			}
		case err := <-httpErrors:
			if err != nil && err != http.ErrServerClosed {
				return err
			}
		case <-ctx.Done():
		}
	} else if httpErrors != nil {
		select {
		case err := <-httpErrors:
			if err != nil && err != http.ErrServerClosed {
				return err
			}
		case <-ctx.Done():
		}
	} else {
		<-ctx.Done()
	}
	if w.consumer != nil {
		_ = w.consumer.Close()
	}
	if w.runCode != nil {
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = w.runCode.Shutdown(shutdown)
	}
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
	if w.runCode != nil {
		_ = w.runCode.Shutdown(ctx)
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

var _ transport.Server = (*Worker)(nil)

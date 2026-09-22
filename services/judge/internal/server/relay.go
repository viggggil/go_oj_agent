package server

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/go-kratos/kratos/v3/transport"

	"github.com/viggggil/go_oj_agent/services/judge/internal/biz"
	"github.com/viggggil/go_oj_agent/services/judge/internal/conf"
)

type RelayServer struct {
	relay  *biz.OutboxRelay
	log    *slog.Logger
	mu     sync.Mutex
	done   chan struct{}
	cancel context.CancelFunc
}

func NewRelayServer(relay *biz.OutboxRelay, config *conf.Bootstrap) *RelayServer {
	if relay == nil {
		return &RelayServer{}
	}
	relayConfig := biz.OutboxRelayConfig{Owner: relayOwner()}
	if config != nil && config.GetMessaging() != nil && config.GetMessaging().GetRabbitmq() != nil {
		rabbit := config.GetMessaging().GetRabbitmq()
		relayConfig.MaxRetries = int(rabbit.GetMaxRetries())
		if delay, err := time.ParseDuration(rabbit.GetRetryDelay()); err == nil {
			relayConfig.RetryDelay = delay
		}
	}
	relay.Configure(relayConfig)
	return &RelayServer{relay: relay, log: slog.Default()}
}

func (s *RelayServer) Start(ctx context.Context) error {
	if s == nil || s.relay == nil {
		return fmt.Errorf("outbox relay server is not configured")
	}
	s.mu.Lock()
	if s.done != nil {
		s.mu.Unlock()
		return fmt.Errorf("outbox relay server already started")
	}
	done := make(chan struct{})
	runCtx, cancel := context.WithCancel(ctx)
	s.done = done
	s.cancel = cancel
	s.mu.Unlock()
	defer cancel()
	defer close(done)

	config := s.relay.Config()
	ticker := time.NewTicker(config.PollInterval)
	defer ticker.Stop()
	for {
		if _, err := s.relay.RunOnce(runCtx); err != nil && runCtx.Err() == nil && s.log != nil {
			s.log.Warn("judge outbox relay iteration failed", "error", err)
		}
		select {
		case <-runCtx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (s *RelayServer) Stop(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	done := s.done
	cancel := s.cancel
	s.mu.Unlock()
	if done == nil {
		return nil
	}
	if cancel != nil {
		cancel()
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func relayOwner() string {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "judge"
	}
	return fmt.Sprintf("%s-%d", hostname, os.Getpid())
}

var _ transport.Server = (*RelayServer)(nil)

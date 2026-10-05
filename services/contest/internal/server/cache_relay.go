package server

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/viggggil/go_oj_agent/services/contest/internal/biz"
	"github.com/viggggil/go_oj_agent/services/contest/internal/data"
)

// Optional write worker. Cache failures are retried by the Outbox; Start never
// requires Redis connectivity and cannot stop authoritative result processing.
type CacheRelayServer struct {
	enabled bool
	relay   *biz.CacheRelay
	mu      sync.Mutex
	cancel  context.CancelFunc
	done    chan struct{}
}

func NewCacheRelayServer(repo *data.Repository, cache *data.LeaderboardRedis) *CacheRelayServer {
	return &CacheRelayServer{enabled: cache.Enabled(), relay: &biz.CacheRelay{Repository: repo, Sink: cache}}
}
func (s *CacheRelayServer) Start(ctx context.Context) error {
	if !s.enabled {
		return nil
	}
	ctx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	if s.done != nil {
		s.mu.Unlock()
		cancel()
		return fmt.Errorf("cache relay already started")
	}
	s.cancel = cancel
	s.done = make(chan struct{})
	done := s.done
	s.mu.Unlock()
	defer close(done)
	defer cancel()
	for {
		// Limit outage/cold-cache work per polling round. Claim only one event at a
		// time, so leases cannot expire while queued behind slow Redis requests.
		for range 32 {
			if ctx.Err() != nil {
				return nil
			}
			claimed, err := s.relay.RunOnce(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				slog.Warn("leaderboard cache relay retry", "error", err)
				break
			}
			if !claimed {
				break
			}
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}
func (s *CacheRelayServer) Stop(ctx context.Context) error {
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

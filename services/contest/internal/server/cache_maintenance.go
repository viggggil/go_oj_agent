package server

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/viggggil/go_oj_agent/services/contest/internal/data"
)

// Independent of the relay: a long snapshot build cannot delay Outbox delivery.
// Workers share application lifetime, and Stop cancels SQL/Redis work and waits.
type CacheMaintenanceServer struct {
	repo   *data.Repository
	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func NewCacheMaintenanceServer(repo *data.Repository) *CacheMaintenanceServer {
	return &CacheMaintenanceServer{repo: repo}
}
func (s *CacheMaintenanceServer) Start(ctx context.Context) error {
	if !s.repo.CacheEnabled() {
		return nil
	}
	run, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	if s.done != nil {
		s.mu.Unlock()
		cancel()
		return fmt.Errorf("cache maintenance already started")
	}
	s.cancel = cancel
	s.done = make(chan struct{})
	done := s.done
	s.mu.Unlock()
	defer cancel()
	defer close(done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		s.repo.MaintainLeaderboards(run)
		select {
		case <-run.Done():
			return nil
		case <-ticker.C:
		}
	}
}
func (s *CacheMaintenanceServer) Stop(ctx context.Context) error {
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

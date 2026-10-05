package server

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/viggggil/go_oj_agent/services/contest/internal/biz"
	"github.com/viggggil/go_oj_agent/services/contest/internal/data"
)

type relayTestRepo struct {
	mu      sync.Mutex
	claimed bool
	failure chan string
}

func (r *relayTestRepo) ClaimCacheEvent(context.Context, string, time.Duration) (*biz.CacheOutboxEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.claimed {
		return nil, nil
	}
	r.claimed = true
	return &biz.CacheOutboxEvent{ID: 1, ContestID: 1, UserID: 1, Version: "1", Payload: []byte(`{"schema":1,"contest_id":1,"user_id":1,"version":"1","solved_count":0,"penalty_seconds":0,"problems":[{"problem_id":1,"solved":false,"wrong_attempts":0,"penalty_seconds":0}]}`)}, nil
}
func (r *relayTestRepo) CompleteCacheEvent(context.Context, int64, string) error {
	return errors.New("unexpected completion")
}
func (r *relayTestRepo) FailCacheEvent(_ context.Context, _ int64, _ string, delay time.Duration, dead bool, reason string) error {
	if dead || delay != time.Second {
		return errors.New("transient Redis failure was not scheduled for retry")
	}
	r.failure <- reason
	return nil
}

type offlineLeaderboardSink struct{}

func (offlineLeaderboardSink) Apply(context.Context, biz.LeaderboardSnapshot) error {
	return errors.New("Redis offline")
}

func TestCacheRelayServerLifecycle(t *testing.T) {
	repo := &relayTestRepo{failure: make(chan string, 1)}
	s := &CacheRelayServer{enabled: true, relay: &biz.CacheRelay{Repository: repo, Sink: offlineLeaderboardSink{}}}
	done := make(chan error, 1)
	go func() { done <- s.Start(t.Context()) }()
	select {
	case reason := <-repo.failure:
		if reason != "Redis offline" {
			t.Fatal(reason)
		}
	case err := <-done:
		t.Fatalf("Redis failure stopped service: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("relay did not record failure")
	}
	if err := s.Start(t.Context()); err == nil {
		t.Fatal("duplicate Start succeeded")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := s.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := s.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}
func TestCacheRelayServerDisabled(t *testing.T) {
	s := NewCacheRelayServer(nil, &data.LeaderboardRedis{})
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := s.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
}

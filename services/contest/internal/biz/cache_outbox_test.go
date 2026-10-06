package biz

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type relayStore struct {
	event                   *CacheOutboxEvent
	completeErr             error
	completed, failed, dead bool
	delay                   time.Duration
}

func (s *relayStore) ClaimCacheEvent(context.Context, string, time.Duration) (*CacheOutboxEvent, error) {
	return s.event, nil
}
func (s *relayStore) CompleteCacheEvent(context.Context, int64, string) error {
	s.completed = true
	return s.completeErr
}
func (s *relayStore) FailCacheEvent(_ context.Context, _ int64, _ string, delay time.Duration, dead bool, _ string) error {
	s.failed = true
	s.dead = dead
	s.delay = delay
	return nil
}

type relaySink struct {
	calls int
	err   error
}

func (s *relaySink) Apply(context.Context, LeaderboardSnapshot) error { s.calls++; return s.err }
func TestCacheRelayReplaysAfterConfirmationFailure(t *testing.T) {
	at := time.Now().UTC()
	s := LeaderboardSnapshot{Schema: 1, ContestID: 20, UserID: 42, Version: "9007199254740993", SolvedCount: 1, PenaltySeconds: 60, Problems: []LeaderboardProblem{{ProblemID: 7, Solved: true, AcceptedAt: &at, PenaltySeconds: 60}}}
	payload, _ := json.Marshal(s)
	store := &relayStore{event: &CacheOutboxEvent{ID: 1, ContestID: 20, UserID: 42, Version: s.Version, Payload: payload}, completeErr: ErrLeaseLost}
	sink := &relaySink{}
	r := CacheRelay{Repository: store, Sink: sink}
	if _, err := r.RunOnce(t.Context()); !errors.Is(err, ErrLeaseLost) {
		t.Fatal(err)
	}
	store.completeErr = nil
	if _, err := r.RunOnce(t.Context()); err != nil || sink.calls != 2 {
		t.Fatalf("calls=%d err=%v", sink.calls, err)
	}
	sink.err = errors.New("redis offline")
	store.event.Retries = 20
	if _, err := r.RunOnce(t.Context()); err != nil || !store.failed || store.dead || store.delay != time.Minute {
		t.Fatalf("retry=%+v err=%v", store, err)
	}
	store.event.Payload = []byte(`{}`)
	store.failed = false
	sink.err = nil
	if _, err := r.RunOnce(t.Context()); err != nil || !store.dead || sink.calls != 3 {
		t.Fatalf("invalid payload reached sink: calls=%d err=%v", sink.calls, err)
	}
}

func TestRelayObservesScheduledSinkFailure(t *testing.T) {
	s := LeaderboardSnapshot{Schema: 1, ContestID: 20, UserID: 42, Version: "1", Problems: []LeaderboardProblem{{ProblemID: 7}}}
	payload, _ := json.Marshal(s)
	store := &relayStore{event: &CacheOutboxEvent{ID: 1, ContestID: s.ContestID, UserID: s.UserID, Version: s.Version, Payload: payload}}
	sink := &relaySink{err: errors.New("Redis disconnected")}
	var observed string
	relay := &CacheRelay{Repository: store, Sink: sink, Observe: func(outcome string, _ time.Duration) { observed = outcome }}
	if claimed, err := relay.RunOnce(t.Context()); err != nil || !claimed {
		t.Fatalf("claimed=%v error=%v", claimed, err)
	}
	if observed != "retry" || !store.failed {
		t.Fatalf("outcome=%s failed=%v", observed, store.failed)
	}
}

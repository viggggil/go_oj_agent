package server

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/viggggil/go_oj_agent/services/judge/internal/biz"
)

func TestRelayServerLifecycle(t *testing.T) {
	repository := &relayRepositoryFake{}
	relay := biz.NewOutboxRelay(repository, relayPublisherFake{})
	relay.Configure(biz.OutboxRelayConfig{Owner: "test-relay", PollInterval: 5 * time.Millisecond})
	server := NewRelayServer(relay, nil)
	done := make(chan error, 1)
	go func() { done <- server.Start(context.Background()) }()

	deadline := time.Now().Add(time.Second)
	for repository.claims.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if repository.claims.Load() == 0 {
		t.Fatal("relay server did not run an outbox iteration")
	}
	stopContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.Stop(stopContext); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Start() error = %v", err)
	}
}

type relayRepositoryFake struct{ claims atomic.Int32 }

func (r *relayRepositoryFake) ClaimOutbox(context.Context, string, time.Time, time.Time, int) ([]biz.OutboxEvent, error) {
	r.claims.Add(1)
	return nil, nil
}
func (*relayRepositoryFake) MarkOutboxPublished(context.Context, int64, string, time.Time) error {
	return nil
}
func (*relayRepositoryFake) MarkOutboxFailure(context.Context, int64, string, time.Time, bool, string) error {
	return nil
}

type relayPublisherFake struct{}

func (relayPublisherFake) Publish(context.Context, biz.PublishedMessage) error { return nil }

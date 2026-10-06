package server

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCacheMetricsExposesOnlyOperationalCounters(t *testing.T) {
	for _, path := range []string{"/metrics", "/debug/vars"} {
		response := httptest.NewRecorder()
		metricsHandler().ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		if response.Code != 200 || !strings.Contains(response.Body.String(), "cache_hits") || !strings.Contains(response.Body.String(), "outbox_oldest_microseconds") {
			t.Fatalf("metrics: %d %s", response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), "password") || strings.Contains(response.Body.String(), "contest_id") {
			t.Fatal("metrics contains sensitive or high cardinality data")
		}
	}
	response := httptest.NewRecorder()
	metricsHandler().ServeHTTP(response, httptest.NewRequest("GET", "/debug/pprof/", nil))
	if response.Code != 404 {
		t.Fatal("unexpected debug handler")
	}
}

func TestMetricsServerStopsCleanlyAndAllowsDisabling(t *testing.T) {
	disabled := NewMetricsServer(nil)
	if err := disabled.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := disabled.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	s := &MetricsServer{address: "127.0.0.1:0"}
	done := make(chan error, 1)
	go func() { done <- s.Start(t.Context()) }()
	deadline := time.Now().Add(time.Second)
	for {
		s.mu.Lock()
		ready := s.server != nil
		s.mu.Unlock()
		if ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("metrics server did not start")
		}
		time.Sleep(time.Millisecond)
	}
	if err := s.Start(t.Context()); err == nil {
		t.Fatal("duplicate start succeeded")
	}
	if err := s.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

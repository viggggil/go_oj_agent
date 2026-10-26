package server

import (
	"errors"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/viggggil/go_oj_agent/services/contest/internal/data"
)

func TestCacheMetricsExposesOnlyOperationalCounters(t *testing.T) {
	data.RecordProjectionResult(nil, 25*time.Millisecond)
	data.RecordProjectionResult(errors.New("private detail"), 7*time.Second)
	for _, path := range []string{"/metrics", "/debug/vars"} {
		response := httptest.NewRecorder()
		metricsHandler().ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		if response.Code != 200 {
			t.Fatalf("metrics: %d %s", response.Code, response.Body.String())
		}
		if path == "/metrics" {
			if !strings.Contains(response.Body.String(), "oj_contest_cache_hit_total") || !strings.Contains(response.Body.String(), "oj_contest_cache_outbox_oldest_age_seconds") || !strings.Contains(response.Body.String(), "oj_contest_projection_processed_event_total") || !strings.Contains(response.Body.String(), "oj_contest_projection_duration_seconds_bucket") || !strings.Contains(response.Body.String(), "outcome=\"success\"") || !strings.Contains(response.Body.String(), "outcome=\"error\"") {
				t.Fatalf("Prometheus metric names or projection outcomes missing: %s", response.Body.String())
			}
		}
		if path == "/debug/vars" && (!strings.Contains(response.Body.String(), "cache_hits") || !strings.Contains(response.Body.String(), "outbox_oldest_microseconds")) {
			t.Fatal("legacy expvar names were not preserved")
		}
		if strings.Contains(response.Body.String(), "password") || strings.Contains(response.Body.String(), "contest_id") {
			t.Fatal("metrics contains sensitive or high cardinality data")
		}
		if strings.Contains(response.Body.String(), "private detail") {
			t.Fatal("metrics contains raw error detail")
		}
	}
	response := httptest.NewRecorder()
	metricsHandler().ServeHTTP(response, httptest.NewRequest("GET", "/debug/pprof/", nil))
	if response.Code != 404 {
		t.Fatal("unexpected debug handler")
	}
}

func TestProjectionHistogramConcurrentScrapesStayMonotonic(t *testing.T) {
	var workers sync.WaitGroup
	for range 4 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range 5000 {
				data.RecordProjectionResult(nil, 25*time.Millisecond)
				data.RecordProjectionResult(errors.New("transient"), 7*time.Second)
			}
		}()
	}
	finished := make(chan struct{})
	go func() { workers.Wait(); close(finished) }()
	for {
		assertProjectionHistogramConsistent(t, data.PrometheusMetrics())
		select {
		case <-finished:
			assertProjectionHistogramConsistent(t, data.PrometheusMetrics())
			return
		default:
		}
	}
}

func assertProjectionHistogramConsistent(t *testing.T, exposition string) {
	t.Helper()
	buckets := map[string][]int64{"success": {}, "error": {}}
	counts := map[string]int64{}
	for _, line := range strings.Split(exposition, "\n") {
		if !strings.HasPrefix(line, "oj_contest_projection_duration_seconds_bucket{") && !strings.HasPrefix(line, "oj_contest_projection_duration_seconds_count{") {
			continue
		}
		outcome := "success"
		if strings.Contains(line, `outcome="error"`) {
			outcome = "error"
		}
		_, rawValue, ok := strings.Cut(line, " ")
		if !ok {
			t.Fatalf("malformed histogram sample %q", line)
		}
		value, err := strconv.ParseInt(rawValue, 10, 64)
		if err != nil {
			t.Fatalf("malformed histogram value in %q: %v", line, err)
		}
		if strings.Contains(line, "_bucket{") {
			buckets[outcome] = append(buckets[outcome], value)
		} else {
			counts[outcome] = value
		}
	}
	for _, outcome := range []string{"success", "error"} {
		values := buckets[outcome]
		if len(values) != 7 || values[len(values)-1] != counts[outcome] {
			t.Fatalf("invalid %s histogram buckets=%v count=%d", outcome, values, counts[outcome])
		}
		for i := 1; i < len(values); i++ {
			if values[i] < values[i-1] {
				t.Fatalf("non-monotonic %s histogram buckets=%v", outcome, values)
			}
		}
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

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/viggggil/go_oj_agent/services/contest/internal/conf"
	"github.com/viggggil/go_oj_agent/services/contest/internal/data"
)

// 独立的观测端口；不暴露业务记录，也不注册默认的 pprof/debug handler。
type MetricsServer struct {
	address string
	server  *http.Server
	mu      sync.Mutex
}

func NewMetricsServer(c *conf.Bootstrap) *MetricsServer {
	return &MetricsServer{address: c.GetServer().GetMetricsAddress()}
}
func metricsHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		metrics := data.CacheMetricsSnapshot()
		keys := make([]string, 0, len(metrics))
		for key := range metrics {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			fmt.Fprintf(w, "contest_leaderboard_%s %d\n", key, metrics[key])
		}
	})
	mux.HandleFunc("/debug/vars", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(data.CacheMetricsSnapshot())
	})
	return mux
}
func (s *MetricsServer) Start(ctx context.Context) error {
	if s.address == "" {
		return nil
	}
	listener, err := net.Listen("tcp", s.address)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if s.server != nil {
		s.mu.Unlock()
		listener.Close()
		return fmt.Errorf("metrics already started")
	}
	s.server = &http.Server{Handler: metricsHandler(), ReadHeaderTimeout: 5 * time.Second}
	srv := s.server
	s.mu.Unlock()
	err = srv.Serve(listener)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
func (s *MetricsServer) Stop(ctx context.Context) error {
	s.mu.Lock()
	srv := s.server
	s.mu.Unlock()
	if srv == nil {
		return nil
	}
	return srv.Shutdown(ctx)
}

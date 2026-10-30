package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	gatewaymw "github.com/viggggil/go_oj_agent/services/gateway/internal/middleware"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/service"
)

func TestMetricsWindowAndTrustedURLs(t *testing.T) {
	for input, want := range map[string]string{"": "1h", "15m": "15m", "1h": "1h", "24h": "24h"} {
		window, ok := parseMetricsWindow(input)
		if !ok || window.name != want || window.step <= 0 || window.duration <= 0 {
			t.Fatalf("window %q = %+v, ok=%t", input, window, ok)
		}
	}
	for _, input := range []string{"7d", "start=now", "15m&window=1h"} {
		if _, ok := parseMetricsWindow(input); ok {
			t.Fatalf("unsupported window %q accepted", input)
		}
	}
	for _, input := range []string{"", "javascript:alert(1)", "https://user:pass@prometheus.local", "http://prometheus.local/?query=up", "file:///etc/passwd"} {
		if _, err := parsePrometheusURL(input); err == nil {
			t.Fatalf("unsafe Prometheus URL %q accepted", input)
		}
	}
	if got := configuredGrafanaDashboard("https://grafana.example/ops"); got != "https://grafana.example/ops/d/oj-system-overview/oj-system-overview" {
		t.Fatalf("system dashboard link = %q", got)
	}
}

func TestObservabilityServicesEndpointRoleScopeAndNoURLInput(t *testing.T) {
	ready := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/readyz" {
			t.Errorf("probe path = %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ready.Close()
	t.Setenv("OBSERVABILITY_PROBE_JUDGE_WORKER_ENDPOINT", ready.URL)
	t.Setenv("OBSERVABILITY_PROBE_RAG_WORKER_ENDPOINT", ready.URL)

	auth, err := gatewaymw.NewAuthMiddleware(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHTTPServer(testConfig(), auth, service.NewGatewayService(nil, nil))
	get := func(roles []string, suffix string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, observabilityServicesPath+suffix, nil)
		if roles != nil {
			req.Header.Set("Authorization", "Bearer "+testAccessTokenRoles(t, roles))
		}
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, req)
		return result
	}
	system := get([]string{"system_admin"}, "")
	if system.Code != http.StatusOK {
		t.Fatalf("system status=%d body=%s", system.Code, system.Body.String())
	}
	var systemResponse observabilityServicesResponse
	if err := json.Unmarshal(system.Body.Bytes(), &systemResponse); err != nil {
		t.Fatal(err)
	}
	if systemResponse.Scope != "system" || len(systemResponse.Services) != len(observabilityServiceCatalog) {
		t.Fatalf("system response=%+v", systemResponse)
	}
	for _, item := range systemResponse.Services {
		if (item.Name == "judge-worker" || item.Name == "rag-worker") && item.State != "up" {
			t.Fatalf("worker health probe did not report up: %+v", item)
		}
		if item.State == "up" && item.Name != "gateway" && (item.LatencyMS == nil || item.LastCheckedAt == nil) {
			t.Fatalf("probed service is missing observation metadata: %+v", item)
		}
	}
	agent := get([]string{"agent_admin"}, "")
	var agentResponse observabilityServicesResponse
	if agent.Code != http.StatusOK || json.Unmarshal(agent.Body.Bytes(), &agentResponse) != nil {
		t.Fatalf("agent status=%d body=%s", agent.Code, agent.Body.String())
	}
	if agentResponse.Scope != "agent" || len(agentResponse.Services) != 2 {
		t.Fatalf("agent scope leaked services: %+v", agentResponse)
	}
	for _, item := range agentResponse.Services {
		if item.Name != "agent" && item.Name != "rag-worker" {
			t.Fatalf("agent scope leaked service %q", item.Name)
		}
	}
	for _, roles := range [][]string{{"user"}, nil} {
		got := get(roles, "")
		if got.Code == http.StatusOK {
			t.Fatalf("roles=%v accessed service health", roles)
		}
	}
	if got := get([]string{"system_admin"}, "?url=http://127.0.0.1:1"); got.Code != http.StatusBadRequest {
		t.Fatalf("arbitrary probe URL status=%d body=%s", got.Code, got.Body.String())
	}
}

func TestFixedServiceProbeResultsAreSanitized(t *testing.T) {
	ready := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/readyz" {
			t.Errorf("probe path = %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ready.Close()
	failed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("private upstream details"))
	}))
	defer failed.Close()

	if err := probeHTTP(context.Background(), ready.URL); err != nil {
		t.Fatalf("ready probe failed: %v", err)
	}
	if err := probeHTTP(context.Background(), failed.URL); err == nil || err.Error() != "service_probe_failed" {
		t.Fatalf("failed probe error = %v", err)
	}
	if err := probeHTTP(context.Background(), "http://user:secret@example.test"); err == nil || err.Error() != "invalid_probe_endpoint" {
		t.Fatalf("invalid endpoint error = %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	defer listener.Close()
	if err := probeTCP(context.Background(), address); err != nil {
		t.Fatalf("reachable TCP probe failed: %v", err)
	}
	if err := probeTCP(context.Background(), "invalid-endpoint"); err == nil || err.Error() != "invalid_probe_endpoint" {
		t.Fatalf("invalid TCP endpoint error = %v", err)
	}
}

func TestMetricsOverviewRoleScopeAndFixedQueries(t *testing.T) {
	var mu sync.Mutex
	var rangeQueries []string
	var targetQueries []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		query := r.URL.Query().Get("query")
		switch r.URL.Path {
		case "/api/v1/query_range":
			mu.Lock()
			rangeQueries = append(rangeQueries, query)
			mu.Unlock()
			var body string
			if !strings.Contains(query, "oj_judge_submission_total") {
				body = `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"series":"agent_run_in_flight"},"values":[[1793088000,"3"]]},{"metric":{"series":"agent_model_failure_rate","service_name":"f04a8e2b-9a04-4f01-9a6d-c32db22741af"},"values":[[1793088000,"0.25"]]}]}}`
			} else {
				body = `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"series":"request_rate","service_name":"gateway-service"},"values":[[1793088000,"2.5"]]},{"metric":{"series":"judge_queue_p95","user_id":"private-user-id"},"values":[[1793088000,"0.2"]]},{"metric":{"series":"request_error_rate","service_name":"private-user-id"},"values":[[1793088000,"0.1"]]},{"metric":{"series":"agent_run_success_ratio"},"values":[[1793088000,"1"]]},{"metric":{"series":"agent_run_ttft_p95"},"values":[[1793088000,"0.3"]]},{"metric":{"series":"agent_model_failure_rate"},"values":[[1793088000,"0"]]}]}}`
			}
			_, _ = w.Write([]byte(body))
		case "/api/v1/query":
			mu.Lock()
			targetQueries = append(targetQueries, query)
			mu.Unlock()
			now := float64(time.Now().Unix())
			jobs := []string{"otel-collector", "otel-collector-self"}
			if strings.Contains(query, "contest-service") {
				jobs = append(jobs, "contest-service")
			}
			var results []map[string]any
			for _, job := range jobs {
				results = append(results,
					map[string]any{"metric": map[string]string{"job": job, "series": "target_up"}, "value": []any{now, "1"}},
					map[string]any{"metric": map[string]string{"job": job, "series": "target_scrape_timestamp"}, "value": []any{now, strconvFloat(now)}},
				)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "vector", "result": results}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	t.Setenv("OBSERVABILITY_PROMETHEUS_URL", upstream.URL)
	t.Setenv("OBSERVABILITY_GRAFANA_URL", "http://localhost:3000")

	auth, err := gatewaymw.NewAuthMiddleware(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHTTPServer(testConfig(), auth, service.NewGatewayService(nil, nil))
	get := func(roles []string, suffix string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, metricsOverviewPath+suffix, nil)
		if roles != nil {
			req.Header.Set("Authorization", "Bearer "+testAccessTokenRoles(t, roles))
		}
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, req)
		return result
	}

	system := get([]string{"system_admin"}, "?window=15m")
	if system.Code != http.StatusOK {
		t.Fatalf("system status=%d body=%s", system.Code, system.Body.String())
	}
	var systemResult metricsOverview
	if err := json.Unmarshal(system.Body.Bytes(), &systemResult); err != nil {
		t.Fatal(err)
	}
	if systemResult.Scope != "system" || systemResult.Window != "15m" || len(systemResult.Targets) != 3 || systemResult.GrafanaURL != "http://localhost:3000/d/oj-system-overview/oj-system-overview" {
		t.Fatalf("system response = %+v", systemResult)
	}
	if strings.Contains(system.Body.String(), "private-user-id") || strings.Contains(system.Body.String(), "f04a8e2b") {
		t.Fatalf("uncontrolled Prometheus labels leaked: %s", system.Body.String())
	}
	systemSeries := make(map[string]metricSeries, len(systemResult.Series))
	for _, series := range systemResult.Series {
		systemSeries[series.Key] = series
	}
	for _, key := range []string{"agent_run_success_ratio", "agent_run_ttft_p95", "agent_model_failure_rate"} {
		if series := systemSeries[key]; series.State != "available" || len(series.Points) == 0 {
			t.Fatalf("system scope Agent series %q = %+v, want available sample", key, series)
		}
	}

	agent := get([]string{"agent_admin"}, "?window=1h")
	if agent.Code != http.StatusOK {
		t.Fatalf("Agent admin status=%d body=%s", agent.Code, agent.Body.String())
	}
	var agentResult metricsOverview
	if err := json.Unmarshal(agent.Body.Bytes(), &agentResult); err != nil {
		t.Fatal(err)
	}
	if agentResult.Scope != "agent" || len(agentResult.Targets) != 2 || agentResult.GrafanaURL != "" {
		t.Fatalf("Agent response = %+v", agentResult)
	}
	for _, series := range agentResult.Series {
		if !strings.HasPrefix(series.Key, "agent_") {
			t.Fatalf("Agent admin received non-Agent series %q", series.Key)
		}
	}

	for _, roles := range [][]string{{"user"}, nil} {
		forbidden := get(roles, "")
		want := http.StatusForbidden
		if roles == nil {
			want = http.StatusUnauthorized
		}
		if forbidden.Code != want {
			t.Fatalf("roles=%v status=%d want=%d", roles, forbidden.Code, want)
		}
	}
	invalid := get([]string{"admin"}, "?query=up")
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("arbitrary query status=%d body=%s", invalid.Code, invalid.Body.String())
	}

	mu.Lock()
	defer mu.Unlock()
	if len(rangeQueries) != 2 || !strings.Contains(rangeQueries[0], "oj_judge_submission_total") || !strings.Contains(rangeQueries[0], "agent_run_success_ratio") || !strings.Contains(rangeQueries[0], "agent_run_ttft_p95") || !strings.Contains(rangeQueries[0], "agent_model_attempt_total") || strings.Contains(rangeQueries[0], "query=up") || !strings.Contains(rangeQueries[1], "agent_model_attempt_total") {
		t.Fatalf("Prometheus fixed range queries = %#v", rangeQueries)
	}
	if len(targetQueries) != 2 || !strings.Contains(targetQueries[0], "contest-service") || strings.Contains(targetQueries[1], "contest-service") {
		t.Fatalf("Prometheus role-scoped target queries = %#v", targetQueries)
	}
}

func TestDecodeObservabilityAlertsAllowlistAndRedaction(t *testing.T) {
	response := prometheusAlertsResponse{Status: "success"}
	response.Data.Alerts = []struct {
		Labels   map[string]string `json:"labels"`
		State    string            `json:"state"`
		ActiveAt string            `json:"activeAt"`
	}{
		{Labels: map[string]string{"alertname": "OJAgentModelErrorRateHigh", "severity": "warning", "service_name": "private-user-id", "summary": "secret"}, State: "firing", ActiveAt: "2026-10-28T08:00:00Z"},
		{Labels: map[string]string{"alertname": "OJJudgeQueueLatencyHigh", "severity": "critical", "job": "judge-service"}, State: "pending", ActiveAt: "2026-10-28T08:01:00Z"},
		{Labels: map[string]string{"alertname": "UnknownAlert", "severity": "warning"}, State: "firing", ActiveAt: "2026-10-28T08:02:00Z"},
	}
	agent := decodeObservabilityAlerts(response, false)
	if len(agent) != 1 || agent[0].Name != "OJAgentModelErrorRateHigh" || agent[0].Service != "" || agent[0].ErrorCode != "AGENT_MODEL_ERROR_RATE_HIGH" {
		t.Fatalf("agent alerts = %+v", agent)
	}
	system := decodeObservabilityAlerts(response, true)
	if len(system) != 2 || system[1].Service != "judge-service" || system[1].Severity != "critical" {
		t.Fatalf("system alerts = %+v", system)
	}
}

func TestRequestPrometheusAlertsRejectsMissingAndNullAlerts(t *testing.T) {
	for _, body := range []string{
		`{"status":"success"}`,
		`{"status":"success","data":{}}`,
		`{"status":"success","data":{"alerts":null}}`,
		`{"status":"success","data":{"alerts":[{}]}}`,
		`{"status":"success","data":{"alerts":[{"labels":{},"state":"inactive","activeAt":"2026-10-28T08:00:00Z"}]}}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
		base, _ := url.Parse(server.URL)
		_, err := requestPrometheusAlerts(context.Background(), base)
		server.Close()
		if err == nil || err.Error() != "prometheus_invalid_response" {
			t.Errorf("body %s error = %v", body, err)
		}
	}
}

func TestObservabilityAlertsEndpointRejectsInputAndUnauthorized(t *testing.T) {
	auth, err := gatewaymw.NewAuthMiddleware(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHTTPServer(testConfig(), auth, service.NewGatewayService(nil, nil))
	request := httptest.NewRequest(http.MethodGet, observabilityAlertsPath+"?query=up", nil)
	request.Header.Set("Authorization", "Bearer "+testAccessTokenRoles(t, []string{"system_admin"}))
	result := httptest.NewRecorder()
	handler.ServeHTTP(result, request)
	if result.Code != http.StatusBadRequest {
		t.Fatalf("query status=%d body=%s", result.Code, result.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, observabilityAlertsPath, nil)
	request.Header.Set("Authorization", "Bearer "+testAccessTokenRoles(t, []string{"user"}))
	result = httptest.NewRecorder()
	handler.ServeHTTP(result, request)
	if result.Code != http.StatusForbidden {
		t.Fatalf("role status=%d body=%s", result.Code, result.Body.String())
	}
}

func TestMetricsOverviewDistinguishesNoDataAndStaleTarget(t *testing.T) {
	for _, scenario := range []struct {
		name string
		up   string
		want string
	}{
		{name: "no data", up: "1", want: "no_data"},
		{name: "target down", up: "0", want: "stale"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/api/v1/query_range" {
					_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
					return
				}
				value := strconvFloat(float64(time.Now().Unix()))
				results := []map[string]any{}
				for _, job := range []string{"otel-collector", "otel-collector-self", "contest-service"} {
					results = append(results,
						map[string]any{"metric": map[string]string{"job": job, "series": "target_up"}, "value": []any{time.Now().Unix(), scenario.up}},
						map[string]any{"metric": map[string]string{"job": job, "series": "target_scrape_timestamp"}, "value": []any{time.Now().Unix(), value}},
					)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "vector", "result": results}})
			}))
			defer upstream.Close()
			window, _ := parseMetricsWindow("1h")
			got := queryPrometheusMetrics(t.Context(), upstream.URL, window, time.Now().UTC(), true)
			if got.State != scenario.want {
				t.Fatalf("state=%s want=%s response=%+v", got.State, scenario.want, got)
			}
		})
	}
}

func TestDecodeScrapeTargetsPreservesAnyDownReplica(t *testing.T) {
	response := prometheusResponse{Status: "success"}
	response.Data.ResultType = "vector"
	response.Data.Result = append(response.Data.Result,
		struct {
			Metric map[string]string   `json:"metric"`
			Values [][]json.RawMessage `json:"values"`
			Value  []json.RawMessage   `json:"value"`
		}{Metric: map[string]string{"job": "otel-collector", "instance": "a", "series": "target_up"}, Value: []json.RawMessage{json.RawMessage("1"), json.RawMessage(`"1"`)}},
		struct {
			Metric map[string]string   `json:"metric"`
			Values [][]json.RawMessage `json:"values"`
			Value  []json.RawMessage   `json:"value"`
		}{Metric: map[string]string{"job": "otel-collector", "instance": "b", "series": "target_up"}, Value: []json.RawMessage{json.RawMessage("1"), json.RawMessage(`"0"`)}},
	)
	targets, err := decodeScrapeTargets(response, time.Now().UTC(), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) == 0 || targets[0].State != "down" {
		t.Fatalf("target state=%+v, want down when any replica is down", targets)
	}
}

func strconvFloat(value float64) string {
	return strings.TrimRight(strings.TrimRight(strings.TrimSpace(strconv.FormatFloat(value, 'f', 3, 64)), "0"), ".")
}

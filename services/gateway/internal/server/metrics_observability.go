package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	kerrors "github.com/go-kratos/kratos/v3/errors"
	khttp "github.com/go-kratos/kratos/v3/transport/http"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/client"
	gatewaymw "github.com/viggggil/go_oj_agent/services/gateway/internal/middleware"
)

const metricsOverviewPath = "/api/v1/admin/observability/metrics"
const observabilityServicesPath = "/api/v1/admin/observability/services"
const observabilityAlertsPath = "/api/v1/admin/observability/alerts"

const systemMetricsRangeQuery = `label_replace(sum by (service_name) (rate(http_server_request_duration_seconds_count[5m])), "series", "request_rate", "", "")
or label_replace(sum by (service_name) (rate(http_server_request_duration_seconds_count{http_response_status_code=~"5.."}[5m])), "series", "request_error_rate", "", "")
or label_replace(histogram_quantile(0.50, sum by (le, service_name) (rate(http_server_request_duration_seconds_bucket[5m]))), "series", "api_latency_p50", "", "")
or label_replace(histogram_quantile(0.95, sum by (le, service_name) (rate(http_server_request_duration_seconds_bucket[5m]))), "series", "api_latency_p95", "", "")
or label_replace(histogram_quantile(0.99, sum by (le, service_name) (rate(http_server_request_duration_seconds_bucket[5m]))), "series", "api_latency_p99", "", "")
or label_replace(sum(rate(oj_judge_submission_total[5m])), "series", "judge_submission_rate", "", "")
or label_replace(histogram_quantile(0.95, sum by (le) (rate(oj_judge_queue_duration_seconds_bucket[5m]))), "series", "judge_queue_p95", "", "")
or label_replace(sum(oj_judge_worker_in_flight), "series", "judge_worker_in_flight", "", "")
or label_replace(sum(oj_contest_cache_outbox_pending), "series", "contest_cache_outbox_pending", "", "")
or label_replace(sum(oj_contest_cache_outbox_oldest_age_seconds), "series", "contest_cache_outbox_oldest_age", "", "")
or label_replace(sum(rate(agent_run_started_total[5m])), "series", "agent_run_rate", "", "")
or label_replace(sum(agent_run_in_flight), "series", "agent_run_in_flight", "", "")
or label_replace((sum(rate(agent_run_terminal_total{status="completed"}[5m])) or (sum(rate(agent_run_terminal_total[5m])) * 0)) / clamp_min(sum(rate(agent_run_terminal_total[5m])), 1e-9), "series", "agent_run_success_ratio", "", "")
or label_replace(histogram_quantile(0.95, sum by (le) (rate(agent_run_first_event_duration_seconds_bucket[5m]))), "series", "agent_run_ttft_p95", "", "")
or label_replace(sum(rate(agent_model_attempt_total{outcome=~"error|timeout"}[5m])) or (sum(rate(agent_model_attempt_total[5m])) * 0), "series", "agent_model_failure_rate", "", "")
or label_replace(sum(rate(agent_tool_total{outcome=~"error|timeout"}[5m])) or (sum(rate(agent_tool_total[5m])) * 0), "series", "agent_tool_failure_rate", "", "")
or label_replace(sum(rate(agent_rag_total{outcome=~"error|timeout"}[5m])) or (sum(rate(agent_rag_total[5m])) * 0), "series", "agent_rag_failure_rate", "", "")
or label_replace(sum(rate(agent_job_total{status=~"failed|lease_lost"}[5m])) or (sum(rate(agent_job_total[5m])) * 0), "series", "agent_job_failure_rate", "", "")
or label_replace(sum(rate(oj_telemetry_export_failure_total[5m])), "series", "telemetry_export_failure_rate", "", "")`

const agentMetricsRangeQuery = `label_replace(sum(rate(agent_run_started_total[5m])), "series", "agent_run_rate", "", "")
or label_replace(sum(agent_run_in_flight), "series", "agent_run_in_flight", "", "")
or label_replace((sum(rate(agent_run_terminal_total{status="completed"}[5m])) or (sum(rate(agent_run_terminal_total[5m])) * 0)) / clamp_min(sum(rate(agent_run_terminal_total[5m])), 1e-9), "series", "agent_run_success_ratio", "", "")
or label_replace(histogram_quantile(0.95, sum by (le) (rate(agent_run_first_event_duration_seconds_bucket[5m]))), "series", "agent_run_ttft_p95", "", "")
or label_replace(sum(rate(agent_model_attempt_total{outcome=~"error|timeout"}[5m])) or (sum(rate(agent_model_attempt_total[5m])) * 0), "series", "agent_model_failure_rate", "", "")
or label_replace(sum(rate(agent_tool_total{outcome=~"error|timeout"}[5m])) or (sum(rate(agent_tool_total[5m])) * 0), "series", "agent_tool_failure_rate", "", "")
or label_replace(sum(rate(agent_rag_total{outcome=~"error|timeout"}[5m])) or (sum(rate(agent_rag_total[5m])) * 0), "series", "agent_rag_failure_rate", "", "")
or label_replace(sum(rate(agent_job_total{status=~"failed|lease_lost"}[5m])) or (sum(rate(agent_job_total[5m])) * 0), "series", "agent_job_failure_rate", "", "")`

const metricsTargetQuery = `label_replace(up{job=~"otel-collector|otel-collector-self|contest-service"}, "series", "target_up", "", "")
or label_replace(timestamp(scrape_samples_scraped{job=~"otel-collector|otel-collector-self|contest-service"}), "series", "target_scrape_timestamp", "", "")`

const agentMetricsTargetQuery = `label_replace(up{job=~"otel-collector|otel-collector-self"}, "series", "target_up", "", "")
or label_replace(timestamp(scrape_samples_scraped{job=~"otel-collector|otel-collector-self"}), "series", "target_scrape_timestamp", "", "")`

const maxPrometheusResponseBytes = 1 << 20
const prometheusTimeout = 2500 * time.Millisecond

type metricsWindow struct {
	name     string
	duration time.Duration
	step     time.Duration
}

func parseMetricsWindow(value string) (metricsWindow, bool) {
	switch value {
	case "", "1h":
		return metricsWindow{name: "1h", duration: time.Hour, step: time.Minute}, true
	case "15m":
		return metricsWindow{name: value, duration: 15 * time.Minute, step: 15 * time.Second}, true
	case "24h":
		return metricsWindow{name: value, duration: 24 * time.Hour, step: 5 * time.Minute}, true
	default:
		return metricsWindow{}, false
	}
}

type metricPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
}

type metricSeries struct {
	Key     string        `json:"key"`
	Service string        `json:"service,omitempty"`
	Label   string        `json:"label"`
	Unit    string        `json:"unit"`
	State   string        `json:"state"`
	Points  []metricPoint `json:"points"`
}

type scrapeTarget struct {
	Name         string     `json:"name"`
	State        string     `json:"state"`
	LastScrapeAt *time.Time `json:"last_scrape_at"`
}

type metricsOverview struct {
	GeneratedAt time.Time      `json:"generated_at"`
	Window      string         `json:"window"`
	Scope       string         `json:"scope"`
	State       string         `json:"state"`
	ErrorCode   string         `json:"error_code,omitempty"`
	GrafanaURL  string         `json:"grafana_url,omitempty"`
	Targets     []scrapeTarget `json:"targets"`
	Series      []metricSeries `json:"series"`
}

type seriesDefinition struct {
	label string
	unit  string
}

var metricSeriesDefinitions = map[string]seriesDefinition{
	"request_rate":                    {label: "HTTP 请求速率", unit: "请求/秒"},
	"request_error_rate":              {label: "HTTP 5xx 速率", unit: "请求/秒"},
	"api_latency_p50":                 {label: "API 延迟 P50", unit: "秒"},
	"api_latency_p95":                 {label: "API 延迟 P95", unit: "秒"},
	"api_latency_p99":                 {label: "API 延迟 P99", unit: "秒"},
	"judge_submission_rate":           {label: "判题提交速率", unit: "次/秒"},
	"judge_queue_p95":                 {label: "判题排队 P95", unit: "秒"},
	"judge_worker_in_flight":          {label: "Worker 在途任务", unit: "个"},
	"contest_cache_outbox_pending":    {label: "Contest 缓存 Outbox 积压", unit: "个"},
	"contest_cache_outbox_oldest_age": {label: "Contest 最老 Outbox 年龄", unit: "秒"},
	"agent_run_rate":                  {label: "Agent Run 启动速率", unit: "次/秒"},
	"agent_run_in_flight":             {label: "Agent 在途 Run", unit: "个"},
	"agent_run_success_ratio":         {label: "Agent Run 成功率", unit: "比例"},
	"agent_run_ttft_p95":              {label: "Agent TTFT P95", unit: "秒"},
	"agent_model_failure_rate":        {label: "Agent 模型失败速率", unit: "次/秒"},
	"agent_tool_failure_rate":         {label: "Agent Tool 失败速率", unit: "次/秒"},
	"agent_rag_failure_rate":          {label: "Agent RAG 失败速率", unit: "次/秒"},
	"agent_job_failure_rate":          {label: "Agent 后台任务失败速率", unit: "次/秒"},
	"telemetry_export_failure_rate":   {label: "遥测导出失败速率", unit: "次/秒"},
}

var allowedMetricServices = map[string]struct{}{
	"agent-rag-worker": {}, "agent-service": {}, "contest-service": {},
	"gateway-service": {}, "judge-service": {}, "judge-worker": {}, "problem-service": {}, "other": {},
}

type prometheusResponse struct {
	Status    string `json:"status"`
	ErrorType string `json:"errorType"`
	Data      struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string   `json:"metric"`
			Values [][]json.RawMessage `json:"values"`
			Value  []json.RawMessage   `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

func registerMetricsObservabilityRoute(server *khttp.Server) {
	server.Route("").GET(metricsOverviewPath, func(ctx khttp.Context) error {
		khttp.SetOperation(ctx, client.MetricsObservabilityOperation)
		handler := ctx.Middleware(func(requestCtx context.Context, _ interface{}) (interface{}, error) {
			return nil, serveMetricsOverview(requestCtx, ctx)
		})
		_, err := handler(ctx, nil)
		return err
	})
	server.Route("").GET(observabilityServicesPath, func(ctx khttp.Context) error {
		khttp.SetOperation(ctx, client.MetricsObservabilityOperation)
		handler := ctx.Middleware(func(requestCtx context.Context, _ interface{}) (interface{}, error) {
			return nil, serveObservabilityServices(requestCtx, ctx)
		})
		_, err := handler(ctx, nil)
		return err
	})
	server.Route("").GET(observabilityAlertsPath, func(ctx khttp.Context) error {
		khttp.SetOperation(ctx, client.MetricsObservabilityOperation)
		handler := ctx.Middleware(func(requestCtx context.Context, _ interface{}) (interface{}, error) {
			return nil, serveObservabilityAlerts(requestCtx, ctx)
		})
		_, err := handler(ctx, nil)
		return err
	})
}

type observabilityAlert struct {
	Name           string    `json:"name"`
	Severity       string    `json:"severity"`
	State          string    `json:"state"`
	StartedAt      time.Time `json:"started_at"`
	Service        string    `json:"service,omitempty"`
	ErrorCode      string    `json:"error_code"`
	TraceAvailable bool      `json:"trace_available"`
}

type observabilityAlertsResponse struct {
	GeneratedAt time.Time            `json:"generated_at"`
	Scope       string               `json:"scope"`
	State       string               `json:"state"`
	ErrorCode   string               `json:"error_code,omitempty"`
	Alerts      []observabilityAlert `json:"alerts"`
}

type prometheusAlertsResponse struct {
	Status string `json:"status"`
	Data   struct {
		Alerts []struct {
			Labels   map[string]string `json:"labels"`
			State    string            `json:"state"`
			ActiveAt string            `json:"activeAt"`
		} `json:"alerts"`
	} `json:"data"`
}

var observabilityAlertCatalog = map[string]struct {
	errorCode string
	agentOnly bool
}{
	"OJScrapeTargetDown":                {"OBSERVABILITY_SCRAPE_TARGET_DOWN", false},
	"OJJudgeQueueLatencyHigh":           {"JUDGE_QUEUE_LATENCY_HIGH", false},
	"OJContestCacheOutboxOldestPending": {"CONTEST_OUTBOX_PENDING_OLD", false},
	"OJAgentModelErrorRateHigh":         {"AGENT_MODEL_ERROR_RATE_HIGH", true},
}

func serveObservabilityAlerts(requestCtx context.Context, httpCtx khttp.Context) error {
	if httpCtx.Request().URL.RawPath != "" || httpCtx.Request().URL.RawQuery != "" {
		return kerrors.BadRequest("GATEWAY_OBSERVABILITY_INVALID_ARGUMENT", "Alert query does not accept parameters")
	}
	rc, ok := gatewaymw.RequestContextFromContext(requestCtx)
	if !ok || (!platformAdministrator(rc.GetRoles()) && !client.AgentAdministrator(rc.GetRoles())) {
		return kerrors.New(http.StatusForbidden, "GATEWAY_OBSERVABILITY_FORBIDDEN", "Alerts require an observability administrator")
	}
	systemScope := platformAdministrator(rc.GetRoles())
	response := observabilityAlertsResponse{GeneratedAt: time.Now().UTC(), Scope: "agent", State: "not_configured", Alerts: []observabilityAlert{}}
	if systemScope {
		response.Scope = "system"
	}
	configured := os.Getenv("OBSERVABILITY_PROMETHEUS_URL")
	base, err := parsePrometheusURL(configured)
	if err != nil {
		httpCtx.Response().Header().Set("Content-Type", "application/json; charset=utf-8")
		httpCtx.Response().Header().Set("Cache-Control", "no-store")
		return json.NewEncoder(httpCtx.Response()).Encode(response)
	}
	response.State = "unavailable"
	queryCtx, cancel := context.WithTimeout(requestCtx, prometheusTimeout)
	defer cancel()
	result, err := requestPrometheusAlerts(queryCtx, base)
	if err != nil {
		response.ErrorCode = prometheusErrorCode(err)
	} else {
		response.State = "available"
		response.Alerts = decodeObservabilityAlerts(result, systemScope)
	}
	httpCtx.Response().Header().Set("Content-Type", "application/json; charset=utf-8")
	httpCtx.Response().Header().Set("Cache-Control", "no-store")
	return json.NewEncoder(httpCtx.Response()).Encode(response)
}

func requestPrometheusAlerts(ctx context.Context, base *url.URL) (prometheusAlertsResponse, error) {
	var result prometheusAlertsResponse
	pathURL := *base
	pathURL.Path = base.Path + "/api/v1/alerts"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pathURL.String(), nil)
	if err != nil {
		return result, errors.New("prometheus_unavailable")
	}
	client := &http.Client{Timeout: prometheusTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	upstream, err := client.Do(req)
	if err != nil {
		return result, errors.New("prometheus_unavailable")
	}
	defer upstream.Body.Close()
	data, err := io.ReadAll(io.LimitReader(upstream.Body, maxPrometheusResponseBytes+1))
	if upstream.StatusCode != http.StatusOK || err != nil || len(data) > maxPrometheusResponseBytes {
		return result, errors.New("prometheus_invalid_response")
	}
	var envelope struct {
		Status string           `json:"status"`
		Data   *json.RawMessage `json:"data"`
	}
	if json.Unmarshal(data, &envelope) != nil || envelope.Status != "success" || envelope.Data == nil || string(*envelope.Data) == "null" {
		return result, errors.New("prometheus_invalid_response")
	}
	if json.Unmarshal(data, &result) != nil || result.Data.Alerts == nil {
		return result, errors.New("prometheus_invalid_response")
	}
	for _, alert := range result.Data.Alerts {
		if alert.Labels == nil || (alert.State != "firing" && alert.State != "pending") {
			return result, errors.New("prometheus_invalid_response")
		}
		if _, err := time.Parse(time.RFC3339Nano, alert.ActiveAt); err != nil {
			return result, errors.New("prometheus_invalid_response")
		}
	}
	return result, nil
}

func decodeObservabilityAlerts(response prometheusAlertsResponse, systemScope bool) []observabilityAlert {
	alerts := make([]observabilityAlert, 0, len(response.Data.Alerts))
	for _, item := range response.Data.Alerts {
		name := item.Labels["alertname"]
		definition, ok := observabilityAlertCatalog[name]
		if !ok || (!systemScope && !definition.agentOnly) || (item.State != "firing" && item.State != "pending") {
			continue
		}
		activeAt, err := time.Parse(time.RFC3339Nano, item.ActiveAt)
		if err != nil {
			continue
		}
		service := sanitizeAlertService(item.Labels["service_name"])
		if service == "" {
			service = sanitizeAlertService(item.Labels["job"])
		}
		alerts = append(alerts, observabilityAlert{Name: name, Severity: sanitizeAlertSeverity(item.Labels["severity"]), State: item.State, StartedAt: activeAt, Service: service, ErrorCode: definition.errorCode, TraceAvailable: false})
	}
	return alerts
}

func sanitizeAlertSeverity(value string) string {
	if value == "critical" || value == "warning" || value == "info" {
		return value
	}
	return "unknown"
}

func sanitizeAlertService(value string) string {
	if _, ok := allowedMetricServices[value]; ok {
		return value
	}
	switch value {
	case "otel-collector", "otel-collector-self", "contest-service":
		return value
	}
	return ""
}

type observabilityService struct {
	Name          string     `json:"name"`
	State         string     `json:"state"`
	Detail        string     `json:"detail"`
	LastCheckedAt *time.Time `json:"last_checked_at,omitempty"`
	LatencyMS     *int64     `json:"latency_ms,omitempty"`
	ErrorCode     string     `json:"error_code,omitempty"`
}

type observabilityServicesResponse struct {
	GeneratedAt time.Time              `json:"generated_at"`
	Scope       string                 `json:"scope"`
	State       string                 `json:"state"`
	ErrorCode   string                 `json:"error_code,omitempty"`
	Services    []observabilityService `json:"services"`
}

var observabilityServiceCatalog = []struct{ name, env, address, kind string }{
	{"gateway", "", "", "self"},
	{"user", "KRATOS_CLIENTS_USER_ENDPOINT", "", "tcp"},
	{"problem", "KRATOS_CLIENTS_PROBLEM_ENDPOINT", "", "tcp"},
	{"judge", "KRATOS_CLIENTS_SUBMISSION_ENDPOINT", "", "tcp"},
	{"contest", "KRATOS_CLIENTS_CONTEST_ENDPOINT", "contest-service:9005", "tcp"},
	{"agent", "KRATOS_CLIENTS_AGENT_ENDPOINT", "", "http"},
	{"judge-worker", "OBSERVABILITY_PROBE_JUDGE_WORKER_ENDPOINT", "", "http"},
	{"rag-worker", "OBSERVABILITY_PROBE_RAG_WORKER_ENDPOINT", "", "http"},
}

func serveObservabilityServices(requestCtx context.Context, httpCtx khttp.Context) error {
	if httpCtx.Request().URL.RawPath != "" || httpCtx.Request().URL.RawQuery != "" {
		return kerrors.BadRequest("GATEWAY_OBSERVABILITY_INVALID_ARGUMENT", "Service health does not accept query parameters")
	}
	rc, ok := gatewaymw.RequestContextFromContext(requestCtx)
	if !ok || (!platformAdministrator(rc.GetRoles()) && !client.AgentAdministrator(rc.GetRoles())) {
		return kerrors.New(http.StatusForbidden, "GATEWAY_OBSERVABILITY_FORBIDDEN", "Service health requires an observability administrator")
	}
	systemScope := platformAdministrator(rc.GetRoles())
	now := time.Now().UTC()
	response := observabilityServicesResponse{GeneratedAt: now, Scope: "agent", State: "not_configured"}
	if systemScope {
		response.Scope = "system"
	}
	items := make([]struct {
		name, env, address, kind string
	}, 0, len(observabilityServiceCatalog))
	for _, item := range observabilityServiceCatalog {
		if !systemScope && item.name != "agent" && item.name != "rag-worker" {
			continue
		}
		items = append(items, item)
	}
	response.Services = probeObservabilityServices(requestCtx, items, now)
	response.State = "available"
	for _, item := range response.Services {
		if item.State == "not_configured" || item.State == "unknown" {
			response.State = "partial"
		}
	}
	httpCtx.Response().Header().Set("Content-Type", "application/json; charset=utf-8")
	httpCtx.Response().Header().Set("Cache-Control", "no-store")
	return json.NewEncoder(httpCtx.Response()).Encode(response)
}

func probeObservabilityServices(ctx context.Context, catalog []struct {
	name, env, address, kind string
}, now time.Time) []observabilityService {
	results := make([]observabilityService, len(catalog))
	var wait sync.WaitGroup
	for index, item := range catalog {
		wait.Add(1)
		go func(index int, item struct {
			name, env, address, kind string
		}) {
			defer wait.Done()
			result := observabilityService{Name: item.name, State: "not_configured", Detail: "No fixed health probe is configured"}
			if item.kind == "self" {
				result.State = "up"
				result.Detail = "Gateway is serving the health request"
				checked := now
				result.LastCheckedAt = &checked
				results[index] = result
				return
			}
			if item.kind == "none" {
				result.Detail = "This service does not expose an independent health endpoint"
				results[index] = result
				return
			}
			address := strings.TrimSpace(os.Getenv(item.env))
			if address == "" {
				address = item.address
			}
			if address == "" {
				results[index] = result
				return
			}
			probeCtx, cancel := context.WithTimeout(ctx, 900*time.Millisecond)
			defer cancel()
			started := time.Now()
			var err error
			switch item.kind {
			case "tcp":
				err = probeTCP(probeCtx, address)
			case "http":
				err = probeHTTP(probeCtx, address)
			default:
				results[index] = result
				return
			}
			checked := time.Now().UTC()
			latency := time.Since(started).Milliseconds()
			result.LastCheckedAt = &checked
			result.LatencyMS = &latency
			if err != nil {
				result.State = "down"
				result.Detail = "Fixed service probe failed"
				result.ErrorCode = "service_probe_failed"
			} else {
				result.State = "up"
				if item.kind == "tcp" {
					result.Detail = "Service TCP endpoint is reachable; dependency readiness is not implied"
				} else {
					result.Detail = "Service health endpoint is responding"
				}
			}
			results[index] = result
		}(index, item)
	}
	wait.Wait()
	return results
}

func probeTCP(ctx context.Context, address string) error {
	if _, _, err := net.SplitHostPort(address); err != nil {
		return errors.New("invalid_probe_endpoint")
	}
	dialer := net.Dialer{Timeout: 900 * time.Millisecond}
	connection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return errors.New("service_probe_failed")
	}
	return connection.Close()
}

func probeHTTP(ctx context.Context, configured string) error {
	base, err := url.Parse(strings.TrimSpace(configured))
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return errors.New("invalid_probe_endpoint")
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/readyz"
	base.RawPath = ""
	client := &http.Client{
		Timeout:       900 * time.Millisecond,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String(), nil)
	if err != nil {
		return errors.New("invalid_probe_endpoint")
	}
	response, err := client.Do(request)
	if err != nil {
		return errors.New("service_probe_failed")
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return errors.New("service_probe_failed")
	}
	return nil
}

func serveMetricsOverview(requestCtx context.Context, httpCtx khttp.Context) error {
	r := httpCtx.Request()
	if r.URL.RawPath != "" || len(r.URL.RawQuery) > 32 {
		return kerrors.BadRequest("GATEWAY_OBSERVABILITY_INVALID_ARGUMENT", "Invalid metrics query")
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return kerrors.BadRequest("GATEWAY_OBSERVABILITY_INVALID_ARGUMENT", "Invalid metrics query")
	}
	if (len(query) > 0 && (len(query) != 1 || !query.Has("window"))) || (query.Has("window") && len(query["window"]) != 1) {
		return kerrors.BadRequest("GATEWAY_OBSERVABILITY_INVALID_ARGUMENT", "Only the window parameter is supported")
	}
	windowValue := query.Get("window")
	window, ok := parseMetricsWindow(windowValue)
	if !ok {
		return kerrors.BadRequest("GATEWAY_OBSERVABILITY_INVALID_ARGUMENT", "window must be 15m, 1h, or 24h")
	}
	rc, ok := gatewaymw.RequestContextFromContext(requestCtx)
	if !ok || (!platformAdministrator(rc.GetRoles()) && !client.AgentAdministrator(rc.GetRoles())) {
		return kerrors.New(http.StatusForbidden, "GATEWAY_OBSERVABILITY_FORBIDDEN", "Metrics require an observability administrator")
	}

	now := time.Now().UTC()
	systemScope := platformAdministrator(rc.GetRoles())
	response := queryPrometheusMetrics(requestCtx, os.Getenv("OBSERVABILITY_PROMETHEUS_URL"), window, now, systemScope)
	if systemScope {
		response.GrafanaURL = configuredGrafanaDashboard(os.Getenv("OBSERVABILITY_GRAFANA_URL"))
	}
	response.GeneratedAt = now
	httpCtx.Response().Header().Set("Content-Type", "application/json; charset=utf-8")
	httpCtx.Response().Header().Set("Cache-Control", "no-store")
	return json.NewEncoder(httpCtx.Response()).Encode(response)
}

func queryPrometheusMetrics(ctx context.Context, configuredURL string, window metricsWindow, now time.Time, systemScope bool) metricsOverview {
	response := metricsOverview{Window: window.name, Scope: "agent", State: "not_configured", Targets: unknownScrapeTargets(false), Series: emptyMetricSeries(false)}
	query := agentMetricsRangeQuery
	if systemScope {
		response.Scope = "system"
		query = systemMetricsRangeQuery
		response.Series = emptyMetricSeries(true)
	}
	baseURL, err := parsePrometheusURL(configuredURL)
	if err != nil {
		return response
	}
	response.State = "unavailable"
	client := &http.Client{
		Timeout:       prometheusTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	queryCtx, cancel := context.WithTimeout(ctx, prometheusTimeout)
	defer cancel()

	start := now.Add(-window.duration)
	rangeValues := url.Values{
		"query": {query},
		"start": {strconv.FormatFloat(float64(start.UnixNano())/float64(time.Second), 'f', 3, 64)},
		"end":   {strconv.FormatFloat(float64(now.UnixNano())/float64(time.Second), 'f', 3, 64)},
		"step":  {strconv.FormatFloat(window.step.Seconds(), 'f', 3, 64)},
	}
	rangeResult, err := requestPrometheus(queryCtx, client, baseURL, "/api/v1/query_range", rangeValues, "matrix")
	if err != nil {
		response.Targets = unknownScrapeTargets(systemScope)
		response.ErrorCode = prometheusErrorCode(err)
		return response
	}
	response.Series, err = decodeMetricSeries(rangeResult, systemScope)
	if err != nil {
		response.ErrorCode = "prometheus_invalid_response"
		response.Series = emptyMetricSeries(systemScope)
		return response
	}

	targetQuery := agentMetricsTargetQuery
	if systemScope {
		targetQuery = metricsTargetQuery
	}
	targetResult, err := requestPrometheus(queryCtx, client, baseURL, "/api/v1/query", url.Values{"query": {targetQuery}}, "vector")
	if err != nil {
		response.Targets = unknownScrapeTargets(systemScope)
		response.State = "stale"
		response.ErrorCode = prometheusErrorCode(err)
		return response
	}
	response.Targets, err = decodeScrapeTargets(targetResult, now, systemScope)
	if err != nil {
		response.Targets = unknownScrapeTargets(systemScope)
		response.State = "stale"
		response.ErrorCode = "prometheus_invalid_response"
		return response
	}
	response.State = "available"
	if !hasMetricSamples(response.Series) {
		response.State = "no_data"
	}
	for _, target := range response.Targets {
		if target.State != "up" {
			response.State = "stale"
			response.ErrorCode = "prometheus_scrape_target_unhealthy"
			return response
		}
	}
	return response
}

func emptyMetricSeries(systemScope bool) []metricSeries {
	keys := []string{"agent_run_rate", "agent_run_in_flight", "agent_run_success_ratio", "agent_run_ttft_p95", "agent_model_failure_rate", "agent_tool_failure_rate", "agent_rag_failure_rate", "agent_job_failure_rate"}
	if systemScope {
		keys = []string{"request_rate", "request_error_rate", "api_latency_p50", "api_latency_p95", "api_latency_p99", "judge_submission_rate", "judge_queue_p95", "judge_worker_in_flight", "contest_cache_outbox_pending", "contest_cache_outbox_oldest_age", "agent_run_rate", "agent_run_in_flight", "agent_run_success_ratio", "agent_run_ttft_p95", "agent_model_failure_rate", "agent_tool_failure_rate", "agent_rag_failure_rate", "agent_job_failure_rate", "telemetry_export_failure_rate"}
	}
	result := make([]metricSeries, 0, len(keys))
	for _, key := range keys {
		definition := metricSeriesDefinitions[key]
		result = append(result, metricSeries{Key: key, Label: definition.label, Unit: definition.unit, State: "no_data", Points: []metricPoint{}})
	}
	return result
}

func parsePrometheusURL(configured string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(configured))
	if err != nil || parsed == nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("prometheus_not_configured")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = ""
	return parsed, nil
}

func requestPrometheus(ctx context.Context, client *http.Client, base *url.URL, endpoint string, values url.Values, expectedType string) (prometheusResponse, error) {
	var response prometheusResponse
	pathURL := *base
	pathURL.Path = base.Path + endpoint
	pathURL.RawQuery = values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pathURL.String(), nil)
	if err != nil {
		return response, errors.New("prometheus_unavailable")
	}
	upstream, err := client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || ctx.Err() == context.DeadlineExceeded {
			return response, errors.New("prometheus_timeout")
		}
		var timeout net.Error
		if errors.As(err, &timeout) && timeout.Timeout() {
			return response, errors.New("prometheus_timeout")
		}
		return response, errors.New("prometheus_unavailable")
	}
	defer upstream.Body.Close()
	if upstream.StatusCode != http.StatusOK {
		return response, errors.New("prometheus_unavailable")
	}
	data, err := io.ReadAll(io.LimitReader(upstream.Body, maxPrometheusResponseBytes+1))
	if err != nil || len(data) > maxPrometheusResponseBytes || json.Unmarshal(data, &response) != nil || response.Status != "success" || response.Data.ResultType != expectedType {
		return response, errors.New("prometheus_invalid_response")
	}
	return response, nil
}

func decodeMetricSeries(response prometheusResponse, systemScope bool) ([]metricSeries, error) {
	series := make([]metricSeries, 0, len(response.Data.Result)+10)
	keys := make(map[string]bool)
	if len(response.Data.Result) > 50 {
		return nil, errors.New("too_many_series")
	}
	for _, value := range response.Data.Result {
		key := value.Metric["series"]
		definition, ok := metricSeriesDefinitions[key]
		if !ok || (!systemScope && !strings.HasPrefix(key, "agent_")) {
			continue
		}
		service := ""
		if raw := value.Metric["service_name"]; raw != "" {
			if _, allowed := allowedMetricServices[raw]; !allowed {
				service = "other"
			} else {
				service = raw
			}
		}
		if len(value.Values) > 300 {
			return nil, errors.New("too_many_points")
		}
		points := make([]metricPoint, 0, len(value.Values))
		for _, pair := range value.Values {
			if len(pair) != 2 {
				return nil, errors.New("invalid_point")
			}
			var timestamp float64
			var rawValue string
			if json.Unmarshal(pair[0], &timestamp) != nil || json.Unmarshal(pair[1], &rawValue) != nil {
				return nil, errors.New("invalid_point")
			}
			numeric, err := strconv.ParseFloat(rawValue, 64)
			if err != nil || math.IsNaN(numeric) || math.IsInf(numeric, 0) || math.IsNaN(timestamp) || math.IsInf(timestamp, 0) {
				continue
			}
			points = append(points, metricPoint{Timestamp: time.Unix(0, int64(timestamp*float64(time.Second))).UTC(), Value: numeric})
		}
		if len(points) == 0 {
			continue
		}
		series = append(series, metricSeries{Key: key, Service: service, Label: definition.label, Unit: definition.unit, State: "available", Points: points})
		keys[key] = true
	}
	for _, empty := range emptyMetricSeries(systemScope) {
		if !keys[empty.Key] {
			series = append(series, empty)
		}
	}
	return series, nil
}

func decodeScrapeTargets(response prometheusResponse, now time.Time, systemScope bool) ([]scrapeTarget, error) {
	byJob := map[string]*scrapeTarget{
		"collector_exporter":     {Name: "collector_exporter", State: "unknown"},
		"collector_self_metrics": {Name: "collector_self_metrics", State: "unknown"},
	}
	if systemScope {
		byJob["contest_metrics"] = &scrapeTarget{Name: "contest_metrics", State: "unknown"}
	}
	for _, result := range response.Data.Result {
		job := result.Metric["job"]
		name := ""
		switch job {
		case "otel-collector":
			name = "collector_exporter"
		case "otel-collector-self":
			name = "collector_self_metrics"
		case "contest-service":
			name = "contest_metrics"
		default:
			continue
		}
		target := byJob[name]
		if target == nil {
			continue
		}
		if result.Metric["series"] == "target_up" {
			value, err := prometheusVectorValue(result.Value)
			if err != nil {
				return nil, err
			}
			candidate := "up"
			if value != 1 {
				candidate = "down"
			}
			target.State = worseTargetState(target.State, candidate)
		} else if result.Metric["series"] == "target_scrape_timestamp" {
			value, err := prometheusVectorValue(result.Value)
			if err != nil {
				return nil, err
			}
			observed := time.Unix(int64(value), 0).UTC()
			if target.LastScrapeAt == nil || observed.After(*target.LastScrapeAt) {
				target.LastScrapeAt = &observed
			}
			if now.Sub(observed) > 45*time.Second || observed.After(now.Add(5*time.Second)) {
				target.State = worseTargetState(target.State, "stale")
			}
		}
	}
	keys := []string{"collector_exporter", "collector_self_metrics"}
	if systemScope {
		keys = append(keys, "contest_metrics")
	}
	result := make([]scrapeTarget, 0, len(keys))
	for _, key := range keys {
		result = append(result, *byJob[key])
	}
	return result, nil
}

func worseTargetState(current, candidate string) string {
	priority := map[string]int{"unknown": 0, "up": 1, "stale": 2, "down": 3}
	if priority[candidate] > priority[current] {
		return candidate
	}
	if current == "" {
		return candidate
	}
	return current
}

func unknownScrapeTargets(systemScope bool) []scrapeTarget {
	targets := []scrapeTarget{{Name: "collector_exporter", State: "unknown"}, {Name: "collector_self_metrics", State: "unknown"}}
	if systemScope {
		targets = append(targets, scrapeTarget{Name: "contest_metrics", State: "unknown"})
	}
	return targets
}

func prometheusVectorValue(pair []json.RawMessage) (float64, error) {
	if len(pair) != 2 {
		return 0, errors.New("invalid_vector")
	}
	var raw string
	if json.Unmarshal(pair[1], &raw) != nil {
		return 0, errors.New("invalid_vector")
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, errors.New("invalid_vector")
	}
	return value, nil
}

func hasMetricSamples(series []metricSeries) bool {
	for _, value := range series {
		if len(value.Points) > 0 {
			return true
		}
	}
	return false
}

func prometheusErrorCode(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded), err.Error() == "prometheus_timeout":
		return "prometheus_timeout"
	case err.Error() == "prometheus_invalid_response":
		return "prometheus_invalid_response"
	default:
		return "prometheus_unavailable"
	}
}

func configuredGrafanaDashboard(configured string) string {
	base, err := url.Parse(strings.TrimSpace(configured))
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return ""
	}
	uid := "oj-system-overview"
	base.Path = strings.TrimRight(base.Path, "/") + "/d/" + uid + "/" + uid
	base.RawPath = ""
	base.Fragment = ""
	return base.String()
}

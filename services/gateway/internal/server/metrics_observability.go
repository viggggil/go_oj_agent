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
	"time"

	kerrors "github.com/go-kratos/kratos/v3/errors"
	khttp "github.com/go-kratos/kratos/v3/transport/http"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/client"
	gatewaymw "github.com/viggggil/go_oj_agent/services/gateway/internal/middleware"
)

const metricsOverviewPath = "/api/v1/admin/observability/metrics"

const systemMetricsRangeQuery = `label_replace(sum by (service_name) (rate(http_server_request_duration_seconds_count[5m])), "series", "request_rate", "", "")
or label_replace(sum by (service_name) (rate(http_server_request_duration_seconds_count{http_response_status_code=~"5.."}[5m])), "series", "request_error_rate", "", "")
or label_replace(sum(rate(oj_judge_submission_total[5m])), "series", "judge_submission_rate", "", "")
or label_replace(histogram_quantile(0.95, sum by (le) (rate(oj_judge_queue_duration_seconds_bucket[5m]))), "series", "judge_queue_p95", "", "")
or label_replace(sum(oj_judge_worker_in_flight), "series", "judge_worker_in_flight", "", "")
or label_replace(sum(oj_contest_cache_outbox_pending), "series", "contest_cache_outbox_pending", "", "")
or label_replace(sum(oj_contest_cache_outbox_oldest_age_seconds), "series", "contest_cache_outbox_oldest_age", "", "")
or label_replace(sum(rate(agent_run_started_total[5m])), "series", "agent_run_rate", "", "")
or label_replace(sum(agent_run_in_flight), "series", "agent_run_in_flight", "", "")
or label_replace(sum(rate(oj_telemetry_export_failure_total[5m])), "series", "telemetry_export_failure_rate", "", "")`

const agentMetricsRangeQuery = `label_replace(sum(rate(agent_run_started_total[5m])), "series", "agent_run_rate", "", "")
or label_replace(sum(agent_run_in_flight), "series", "agent_run_in_flight", "", "")
or label_replace(sum(rate(agent_model_attempt_total{outcome=~"error|timeout"}[5m])), "series", "agent_model_failure_rate", "", "")
or label_replace(sum(rate(agent_tool_total{outcome=~"error|timeout"}[5m])), "series", "agent_tool_failure_rate", "", "")
or label_replace(sum(rate(agent_rag_total{outcome=~"error|timeout"}[5m])), "series", "agent_rag_failure_rate", "", "")
or label_replace(sum(rate(agent_job_total{status=~"failed|lease_lost"}[5m])), "series", "agent_job_failure_rate", "", "")`

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
	"judge_submission_rate":           {label: "判题提交速率", unit: "次/秒"},
	"judge_queue_p95":                 {label: "判题排队 P95", unit: "秒"},
	"judge_worker_in_flight":          {label: "Worker 在途任务", unit: "个"},
	"contest_cache_outbox_pending":    {label: "Contest 缓存 Outbox 积压", unit: "个"},
	"contest_cache_outbox_oldest_age": {label: "Contest 最老 Outbox 年龄", unit: "秒"},
	"agent_run_rate":                  {label: "Agent Run 启动速率", unit: "次/秒"},
	"agent_run_in_flight":             {label: "Agent 在途 Run", unit: "个"},
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
	keys := []string{"agent_run_rate", "agent_run_in_flight", "agent_model_failure_rate", "agent_tool_failure_rate", "agent_rag_failure_rate", "agent_job_failure_rate"}
	if systemScope {
		keys = []string{"request_rate", "request_error_rate", "judge_submission_rate", "judge_queue_p95", "judge_worker_in_flight", "contest_cache_outbox_pending", "contest_cache_outbox_oldest_age", "agent_run_rate", "agent_run_in_flight", "telemetry_export_failure_rate"}
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

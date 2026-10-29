package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	kerrors "github.com/go-kratos/kratos/v3/errors"
	khttp "github.com/go-kratos/kratos/v3/transport/http"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/client"
	gatewaymw "github.com/viggggil/go_oj_agent/services/gateway/internal/middleware"
)

const observabilityLogsPath = "/api/v1/admin/observability/logs"
const observabilityLogsLinkPath = "/api/v1/admin/observability/logs/link"
const logsQueryTimeout = 2500 * time.Millisecond
const maxLogsResponseBytes = 1 << 20

var traceIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
var spanIDPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)
var logCodePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
var logSecretPattern = regexp.MustCompile(`(?i)(authorization|cookie|password|passwd|token|api[_-]?key|secret|dsn)\s*[:=]\s*[^\s,;]+`)
var bearerPattern = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/-]+=*`)

type observabilityLogEntry struct {
	Timestamp    time.Time `json:"timestamp"`
	Level        string    `json:"level"`
	Service      string    `json:"service_name"`
	Message      string    `json:"message"`
	ErrorCode    string    `json:"error_code,omitempty"`
	TraceID      string    `json:"trace_id,omitempty"`
	SpanID       string    `json:"span_id,omitempty"`
	RunID        string    `json:"run_id,omitempty"`
	SubmissionID string    `json:"submission_id,omitempty"`
}
type observabilityLogsResponse struct {
	GeneratedAt time.Time               `json:"generated_at"`
	Scope       string                  `json:"scope"`
	State       string                  `json:"state"`
	ErrorCode   string                  `json:"error_code,omitempty"`
	Logs        []observabilityLogEntry `json:"logs"`
}
type lokiResponse struct {
	Status string `json:"status"`
	Data   struct {
		Result []struct {
			Stream map[string]string `json:"stream"`
			Values [][]string        `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

func registerLogsObservabilityRoute(server *khttp.Server) {
	server.Route("").GET(observabilityLogsPath, func(ctx khttp.Context) error {
		khttp.SetOperation(ctx, client.MetricsObservabilityOperation)
		h := ctx.Middleware(func(requestCtx context.Context, _ interface{}) (interface{}, error) {
			return nil, serveObservabilityLogs(requestCtx, ctx)
		})
		_, err := h(ctx, nil)
		return err
	})
	server.Route("").GET(observabilityLogsLinkPath, func(ctx khttp.Context) error {
		khttp.SetOperation(ctx, client.MetricsObservabilityOperation)
		h := ctx.Middleware(func(requestCtx context.Context, _ interface{}) (interface{}, error) {
			return nil, serveObservabilityLogsLink(requestCtx, ctx)
		})
		_, err := h(ctx, nil)
		return err
	})
}

func serveObservabilityLogs(ctx context.Context, httpCtx khttp.Context) error {
	rc, ok := gatewaymw.RequestContextFromContext(ctx)
	if !ok || (!platformAdministrator(rc.GetRoles()) && !client.AgentAdministrator(rc.GetRoles())) {
		return kerrors.New(http.StatusForbidden, "GATEWAY_OBSERVABILITY_FORBIDDEN", "Logs require an observability administrator")
	}
	q := httpCtx.Request().URL.Query()
	for key := range q {
		if key != "query" && key != "url" && key != "service" && key != "level" && key != "error_code" && key != "trace_id" && key != "start" && key != "end" && key != "limit" {
			return kerrors.BadRequest("GATEWAY_OBSERVABILITY_INVALID_ARGUMENT", "Unsupported log query parameter")
		}
	}
	if q.Get("query") != "" || q.Get("url") != "" {
		return kerrors.BadRequest("GATEWAY_OBSERVABILITY_INVALID_ARGUMENT", "Arbitrary LogQL is not accepted")
	}
	service, level, code, traceID := q.Get("service"), q.Get("level"), q.Get("error_code"), q.Get("trace_id")
	system := platformAdministrator(rc.GetRoles())
	if !system && service != "" && service != "agent-service" && service != "agent-rag-worker" {
		return kerrors.New(http.StatusForbidden, "GATEWAY_OBSERVABILITY_FORBIDDEN", "Agent administrators may only query Agent logs")
	}
	if service != "" {
		if _, ok := allowedMetricServices[service]; !ok {
			return kerrors.BadRequest("GATEWAY_OBSERVABILITY_INVALID_ARGUMENT", "Unknown service")
		}
	}
	if level != "" && !map[string]bool{"DEBUG": true, "INFO": true, "WARN": true, "ERROR": true}[level] {
		return kerrors.BadRequest("GATEWAY_OBSERVABILITY_INVALID_ARGUMENT", "Unknown log level")
	}
	if code != "" && !logCodePattern.MatchString(code) {
		return kerrors.BadRequest("GATEWAY_OBSERVABILITY_INVALID_ARGUMENT", "Invalid error code")
	}
	if code != "" {
		if !allowedLogErrorCodes[code] {
			return kerrors.BadRequest("GATEWAY_OBSERVABILITY_INVALID_ARGUMENT", "Unknown error code")
		}
	}
	if traceID != "" && !traceIDPattern.MatchString(traceID) {
		return kerrors.BadRequest("GATEWAY_OBSERVABILITY_INVALID_ARGUMENT", "Invalid trace ID")
	}
	end := time.Now().UTC()
	start := end.Add(-15 * time.Minute)
	if raw := q.Get("start"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return kerrors.BadRequest("GATEWAY_OBSERVABILITY_INVALID_ARGUMENT", "start must be RFC3339 UTC")
		}
		start = parsed.UTC()
	}
	if raw := q.Get("end"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return kerrors.BadRequest("GATEWAY_OBSERVABILITY_INVALID_ARGUMENT", "end must be RFC3339 UTC")
		}
		end = parsed.UTC()
	}
	if end.After(time.Now().Add(time.Minute)) || !end.After(start) || end.Sub(start) > 24*time.Hour {
		return kerrors.BadRequest("GATEWAY_OBSERVABILITY_INVALID_ARGUMENT", "time range must be positive and at most 24 hours")
	}
	limit := 100
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 200 {
			return kerrors.BadRequest("GATEWAY_OBSERVABILITY_INVALID_ARGUMENT", "limit must be between 1 and 200")
		}
		limit = n
	}
	response := observabilityLogsResponse{GeneratedAt: time.Now().UTC(), Scope: "agent", State: "not_configured", Logs: []observabilityLogEntry{}}
	if system {
		response.Scope = "system"
	}
	base, err := parseLokiURL(os.Getenv("OBSERVABILITY_LOKI_URL"))
	if err != nil {
		return writeLogsResponse(httpCtx, response)
	}
	query := buildLokiQuery(service, level, code, traceID, system)
	values := url.Values{"query": {query}, "limit": {strconv.Itoa(limit)}, "direction": {"backward"}, "start": {strconv.FormatInt(start.UnixNano(), 10)}, "end": {strconv.FormatInt(end.UnixNano(), 10)}}
	requestCtx, cancel := context.WithTimeout(ctx, logsQueryTimeout)
	defer cancel()
	path := *base
	path.Path = strings.TrimRight(path.Path, "/") + "/loki/api/v1/query_range"
	path.RawQuery = values.Encode()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, path.String(), nil)
	if err != nil {
		return writeLogsResponse(httpCtx, response)
	}
	resp, err := (&http.Client{Timeout: logsQueryTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		response.State = "unavailable"
		response.ErrorCode = "GATEWAY_LOKI_UNAVAILABLE"
		return writeLogsResponse(httpCtx, response)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		response.State = "unavailable"
		response.ErrorCode = "GATEWAY_LOKI_UNAVAILABLE"
		return writeLogsResponse(httpCtx, response)
	}
	var payload lokiResponse
	decoder := json.NewDecoder(ioLimitReader(resp.Body, maxLogsResponseBytes))
	if err := decoder.Decode(&payload); err != nil || payload.Status != "success" {
		response.State = "unavailable"
		response.ErrorCode = "GATEWAY_LOKI_INVALID_RESPONSE"
		return writeLogsResponse(httpCtx, response)
	}
	for _, stream := range payload.Data.Result {
		for _, value := range stream.Values {
			if len(value) != 2 {
				continue
			}
			var entry observabilityLogEntry
			if err := json.Unmarshal([]byte(value[1]), &entry); err != nil || !allowedLogServices[entry.Service] || entry.Message == "" {
				continue
			}
			if entry.Level != "DEBUG" && entry.Level != "INFO" && entry.Level != "WARN" && entry.Level != "ERROR" {
				continue
			}
			if entry.ErrorCode != "" && !allowedLogErrorCodes[entry.ErrorCode] {
				entry.ErrorCode = "OTHER"
			}
			if entry.TraceID != "" && !traceIDPattern.MatchString(entry.TraceID) {
				entry.TraceID = ""
			}
			if entry.SpanID != "" && !spanIDPattern.MatchString(entry.SpanID) {
				entry.SpanID = ""
			}
			entry.Message = sanitizeLogMessage(entry.Message)
			if entry.Service == "agent-service" || entry.Service == "agent-rag-worker" || (system && allowedLogServices[entry.Service]) {
				response.Logs = append(response.Logs, entry)
			}
		}
	}
	if len(response.Logs) > 0 {
		response.State = "available"
	} else {
		response.State = "no_data"
	}
	return writeLogsResponse(httpCtx, response)
}

func serveObservabilityLogsLink(ctx context.Context, httpCtx khttp.Context) error {
	q := httpCtx.Request().URL.Query()
	if q.Get("trace_id") == "" {
		return kerrors.BadRequest("GATEWAY_OBSERVABILITY_INVALID_ARGUMENT", "trace_id is required")
	}
	if !traceIDPattern.MatchString(q.Get("trace_id")) {
		return kerrors.BadRequest("GATEWAY_OBSERVABILITY_INVALID_ARGUMENT", "Invalid trace ID")
	}
	return serveObservabilityLogs(ctx, httpCtx)
}

func buildLokiQuery(service, level, code, traceID string, system bool) string {
	labels := []string{}
	if service != "" {
		labels = append(labels, `service_name="`+service+`"`)
	} else if !system {
		labels = append(labels, `service_name=~"agent-service|agent-rag-worker"`)
	}
	if level != "" {
		labels = append(labels, `level="`+level+`"`)
	}
	if len(labels) == 0 {
		labels = append(labels, `service_name=~".+"`)
	}
	query := `{` + strings.Join(labels, ",") + `}` + ` | json`
	if code != "" {
		query += ` | error_code="` + code + `"`
	}
	if traceID != "" {
		query += ` | trace_id="` + traceID + `"`
	}
	return query
}

var allowedLogServices = map[string]bool{"gateway-service": true, "judge-service": true, "judge-worker": true, "contest-service": true, "agent-service": true, "agent-rag-worker": true, "problem-service": true}
var allowedLogErrorCodes = map[string]bool{
	"OTHER": true, "OTEL_INIT_FAILED": true, "OTEL_SHUTDOWN_FAILED": true,
	"GATEWAY_CONFIG_LOAD_FAILED": true, "GATEWAY_CONFIG_SCAN_FAILED": true, "GATEWAY_AGENT_CONFIG_FAILED": true, "GATEWAY_INIT_FAILED": true, "GATEWAY_EXITED": true,
	"CONTEST_CONFIG_LOAD_FAILED": true, "CONTEST_CONFIG_SCAN_FAILED": true, "CONTEST_CONFIG_INVALID": true, "CONTEST_INIT_FAILED": true, "CONTEST_EXITED": true, "CONTEST_CACHE_RELAY_RETRY": true, "CONTEST_EVENT_INVALID": true, "CONTEST_EVENT_REJECTED": true, "CONTEST_EVENT_REQUEUED": true, "CONTEST_LEADERBOARD_REBUILD_FAILED": true,
	"JUDGE_CONFIG_LOAD_FAILED": true, "JUDGE_CONFIG_SCAN_FAILED": true, "JUDGE_CONFIG_INVALID": true, "JUDGE_INIT_FAILED": true, "JUDGE_EXITED": true, "JUDGE_COMPILE_REJECTED": true, "JUDGE_COMPILE_SANDBOX_FAILURE": true, "JUDGE_EXECUTION_SANDBOX_FAILURE": true, "JUDGE_RUN_CODE_CLEANUP_FAILED": true, "JUDGE_TASK_ACK_FAILED": true, "JUDGE_TASK_FAILED": true, "JUDGE_RESULT_PUBLISH_FAILED": true,
	"AGENT_RUN_FAILED": true, "AGENT_BACKGROUND_FAILED": true, "AGENT_FINALIZATION_FAILED": true, "AGENT_RUNTIME_CLOSE_FAILED": true, "AGENT_MODEL_TIMEOUT": true, "AGENT_MODEL_UNAVAILABLE": true, "AGENT_MODEL_RATE_LIMIT": true, "AGENT_MODEL_REJECTED": true, "AGENT_MODEL_AUTH_FAILED": true, "AGENT_MODEL_QUOTA": true, "AGENT_MODEL_BLOCKED": true, "AGENT_MODEL_INVALID_RESPONSE": true, "AGENT_MODEL_OUTPUT_LIMIT": true, "AGENT_MODEL_INPUT_LIMIT": true, "AGENT_MODEL_CALL_LIMIT": true, "AGENT_MODEL_UNSUPPORTED_OUTPUT": true, "AGENT_CREDENTIAL_UNAVAILABLE": true, "AGENT_STARTUP_FAILED": true, "AGENT_SHUTDOWN_FAILED": true, "AGENT_INTERNAL_ERROR": true, "AGENT_DATABASE_UNAVAILABLE": true, "AGENT_CANCELLED": true,
}

func sanitizeLogMessage(value string) string {
	value = logSecretPattern.ReplaceAllString(value, "$1=[REDACTED]")
	value = bearerPattern.ReplaceAllString(value, "Bearer [REDACTED]")
	value = strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\t' {
			return -1
		}
		return r
	}, value)
	if len(value) > 512 {
		value = value[:512] + "…"
	}
	return value
}

func parseLokiURL(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, fmt.Errorf("Loki is not configured")
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("invalid Loki URL")
	}
	return parsed, nil
}
func writeLogsResponse(ctx khttp.Context, response observabilityLogsResponse) error {
	ctx.Response().Header().Set("Content-Type", "application/json; charset=utf-8")
	ctx.Response().Header().Set("Cache-Control", "no-store")
	return json.NewEncoder(ctx.Response()).Encode(response)
}

type limitedReader struct {
	r io.Reader
	n int64
}

func ioLimitReader(r io.Reader, n int) io.Reader { return &limitedReader{r: r, n: int64(n)} }
func (r *limitedReader) Read(p []byte) (int, error) {
	if r.n <= 0 {
		return 0, fmt.Errorf("response too large")
	}
	if int64(len(p)) > r.n {
		p = p[:r.n]
	}
	n, err := r.r.Read(p)
	r.n -= int64(n)
	return n, err
}

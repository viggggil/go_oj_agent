package server

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	kerrors "github.com/go-kratos/kratos/v3/errors"
	khttp "github.com/go-kratos/kratos/v3/transport/http"
	submissionv1 "github.com/viggggil/go_oj_agent/api/submission/v1"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/client"
	gatewaymw "github.com/viggggil/go_oj_agent/services/gateway/internal/middleware"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/service"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

const traceDetailPath = "/api/v1/admin/observability/traces/{trace_id}"
const judgeTracesPath = "/api/v1/admin/observability/judge/traces"
const maxTraceBytes = 4 << 20
const maxTraceSpans = 2000

type traceSpan struct {
	SpanID       string         `json:"span_id"`
	ParentSpanID string         `json:"parent_span_id,omitempty"`
	Service      string         `json:"service_name"`
	Name         string         `json:"name"`
	Kind         string         `json:"kind"`
	StartedAt    time.Time      `json:"started_at"`
	DurationMS   float64        `json:"duration_ms"`
	Status       string         `json:"status"`
	Attributes   map[string]any `json:"attributes"`
	Events       []traceEvent   `json:"events"`
	Links        []traceLink    `json:"links"`
}
type traceEvent struct {
	Name       string         `json:"name"`
	Timestamp  time.Time      `json:"timestamp"`
	Attributes map[string]any `json:"attributes"`
}
type traceLink struct {
	TraceID string `json:"trace_id"`
	SpanID  string `json:"span_id"`
}
type traceDetailResponse struct {
	TraceID   string      `json:"trace_id"`
	Scope     string      `json:"scope"`
	State     string      `json:"state"`
	ErrorCode string      `json:"error_code,omitempty"`
	Spans     []traceSpan `json:"spans"`
}
type traceSummary struct {
	TraceID    string    `json:"trace_id"`
	Service    string    `json:"service_name"`
	Name       string    `json:"name"`
	StartedAt  time.Time `json:"started_at"`
	DurationMS float64   `json:"duration_ms"`
}
type traceSearchResponse struct {
	State     string         `json:"state"`
	ErrorCode string         `json:"error_code,omitempty"`
	Traces    []traceSummary `json:"traces"`
	Limit     int            `json:"limit"`
}

func registerTraceObservabilityRoutes(server *khttp.Server, gateway *service.GatewayService) {
	for _, path := range []string{traceDetailPath, judgeTracesPath} {
		server.Route("").GET(path, func(ctx khttp.Context) error {
			khttp.SetOperation(ctx, client.MetricsObservabilityOperation)
			h := ctx.Middleware(func(requestCtx context.Context, _ interface{}) (interface{}, error) {
				if ctx.Request().URL.Path == judgeTracesPath {
					return nil, serveJudgeTraces(requestCtx, ctx, gateway)
				}
				return nil, serveTraceDetail(requestCtx, ctx)
			})
			_, err := h(ctx, nil)
			return err
		})
	}
}

func writeTraceJSON(ctx khttp.Context, response any) error {
	ctx.Response().Header().Set("Content-Type", "application/json; charset=utf-8")
	ctx.Response().Header().Set("Cache-Control", "no-store")
	return json.NewEncoder(ctx.Response()).Encode(response)
}

// Tempo 地址只由服务端配置；不跟随重定向，也不把上游错误正文返回给浏览器。
func requestTempo(ctx context.Context, path string, query url.Values, accept string) ([]byte, int, error) {
	base, err := parseLokiURL(os.Getenv("OBSERVABILITY_TEMPO_URL"))
	if err != nil {
		return nil, 0, err
	}
	base.Path = strings.TrimRight(base.Path, "/") + path
	base.RawQuery = query.Encode()
	base.Fragment = ""
	ctx, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String(), nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", accept)
	response, err := (&http.Client{Timeout: 2500 * time.Millisecond, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, response.StatusCode, fmt.Errorf("Tempo query failed")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxTraceBytes+1))
	if err != nil || len(body) > maxTraceBytes {
		return nil, response.StatusCode, fmt.Errorf("invalid Tempo response size")
	}
	return body, response.StatusCode, nil
}

func serveTraceDetail(ctx context.Context, httpCtx khttp.Context) error {
	rc, ok := gatewaymw.RequestContextFromContext(ctx)
	if !ok || !client.AgentAdministrator(rc.GetRoles()) {
		return kerrors.Forbidden("GATEWAY_OBSERVABILITY_FORBIDDEN", "Trace requires an observability administrator")
	}
	traceID := httpCtx.Vars().Get("trace_id")
	if !traceIDPattern.MatchString(traceID) || traceID == strings.Repeat("0", 32) || len(httpCtx.Request().URL.Query()) != 0 {
		return kerrors.BadRequest("GATEWAY_OBSERVABILITY_INVALID_ARGUMENT", "Expected a valid trace ID without query parameters")
	}
	system := platformAdministrator(rc.GetRoles())
	result := traceDetailResponse{TraceID: traceID, Scope: "agent", State: "not_configured", Spans: []traceSpan{}}
	if system {
		result.Scope = "system"
	}
	if _, err := parseLokiURL(os.Getenv("OBSERVABILITY_TEMPO_URL")); err != nil {
		return writeTraceJSON(httpCtx, result)
	}
	body, status, err := requestTempo(ctx, "/api/traces/"+traceID, nil, "application/protobuf")
	if status == http.StatusNotFound {
		result.State = "no_data"
		return writeTraceJSON(httpCtx, result)
	}
	if err != nil {
		result.State, result.ErrorCode = "unavailable", "GATEWAY_TEMPO_UNAVAILABLE"
		return writeTraceJSON(httpCtx, result)
	}
	// Tempo Trace.batches 与 OTLP TracesData.resource_spans 使用相同的 protobuf 字段编号。
	var payload tracepb.TracesData
	if err := proto.Unmarshal(body, &payload); err != nil || len(body) == 0 {
		result.State, result.ErrorCode = "unavailable", "GATEWAY_TEMPO_INVALID_RESPONSE"
		return writeTraceJSON(httpCtx, result)
	}
	result.Spans, err = projectTrace(&payload, traceID, system)
	if err != nil {
		result.Spans = []traceSpan{}
		result.State, result.ErrorCode = "unavailable", "GATEWAY_TEMPO_INVALID_RESPONSE"
	} else if len(result.Spans) == 0 {
		result.State = "no_data"
	} else {
		result.State = "available"
	}
	return writeTraceJSON(httpCtx, result)
}

func projectTrace(payload *tracepb.TracesData, traceID string, system bool) ([]traceSpan, error) {
	result := []traceSpan{}
	seen := map[string]bool{}
	for _, batch := range payload.GetResourceSpans() {
		serviceName := ""
		for _, attr := range batch.GetResource().GetAttributes() {
			if attr.Key == "service.name" {
				serviceName = attr.Value.GetStringValue()
			}
		}
		if _, ok := allowedMetricServices[serviceName]; !ok {
			continue
		}
		if !system && serviceName != "agent-service" && serviceName != "agent-rag-worker" {
			continue
		}
		for _, scope := range batch.GetScopeSpans() {
			for _, span := range scope.GetSpans() {
				id := hex.EncodeToString(span.GetSpanId())
				if hex.EncodeToString(span.GetTraceId()) != traceID || !spanIDPattern.MatchString(id) || id == strings.Repeat("0", 16) || seen[id] {
					continue
				}
				if span.GetStartTimeUnixNano() == 0 || span.GetEndTimeUnixNano() < span.GetStartTimeUnixNano() || span.GetEndTimeUnixNano() > uint64(1<<63-1) {
					return nil, fmt.Errorf("invalid span timestamp")
				}
				if len(result) >= maxTraceSpans {
					return nil, fmt.Errorf("too many spans")
				}
				seen[id] = true
				item := traceSpan{SpanID: id, Service: serviceName, Name: safeTraceText(span.GetName()), Kind: span.GetKind().String(), StartedAt: time.Unix(0, int64(span.GetStartTimeUnixNano())).UTC(), DurationMS: float64(span.GetEndTimeUnixNano()-span.GetStartTimeUnixNano()) / 1e6, Status: span.GetStatus().GetCode().String(), Attributes: projectTraceAttributes(span.GetAttributes()), Events: []traceEvent{}, Links: []traceLink{}}
				if len(span.GetParentSpanId()) == 8 && hex.EncodeToString(span.GetParentSpanId()) != strings.Repeat("0", 16) {
					item.ParentSpanID = hex.EncodeToString(span.GetParentSpanId())
				}
				for _, event := range span.GetEvents() {
					if len(item.Events) == 100 {
						break
					}
					// Exception 正文和 stacktrace 不在属性白名单中。
					item.Events = append(item.Events, traceEvent{Name: safeTraceText(event.GetName()), Timestamp: time.Unix(0, int64(event.GetTimeUnixNano())).UTC(), Attributes: projectTraceAttributes(event.GetAttributes())})
				}
				for _, link := range span.GetLinks() {
					if len(item.Links) == 100 {
						break
					}
					if len(link.GetTraceId()) == 16 && len(link.GetSpanId()) == 8 {
						item.Links = append(item.Links, traceLink{TraceID: hex.EncodeToString(link.GetTraceId()), SpanID: hex.EncodeToString(link.GetSpanId())})
					}
				}
				result = append(result, item)
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].StartedAt.Equal(result[j].StartedAt) {
			return result[i].SpanID < result[j].SpanID
		}
		return result[i].StartedAt.Before(result[j].StartedAt)
	})
	return result, nil
}

var traceAttributeAllowlist = map[string]bool{}

func init() {
	for _, name := range strings.Fields("service.name deployment.environment rpc.system rpc.service rpc.method rpc.grpc.status_code http.request.method http.response.status_code http.method http.status_code http.route server.port messaging.system messaging.operation messaging.event_id messaging.event_type messaging.routing_key messaging.causation_id outbox.event_id outbox.retry_count submission.id submission.status submission.language submission.contest submission.invalidated_id problem.id contest.id judge.operation judge.revision judge.attempt leaderboard.version agent.run_id agent.purpose agent.status agent.model agent.tool agent.error_code agent.attempt error.code error.type gen_ai.system gen_ai.request.model gen_ai.response.model") {
		traceAttributeAllowlist[name] = true
	}
}
func projectTraceAttributes(attrs []*commonpb.KeyValue) map[string]any {
	result := map[string]any{}
	for _, attr := range attrs {
		if !traceAttributeAllowlist[attr.Key] {
			continue
		}
		switch value := attr.GetValue().GetValue().(type) {
		case *commonpb.AnyValue_StringValue:
			result[attr.Key] = safeTraceText(value.StringValue)
		case *commonpb.AnyValue_IntValue:
			result[attr.Key] = value.IntValue
		case *commonpb.AnyValue_BoolValue:
			result[attr.Key] = value.BoolValue
		case *commonpb.AnyValue_DoubleValue:
			if value.DoubleValue < 1e100 && value.DoubleValue > -1e100 {
				result[attr.Key] = value.DoubleValue
			}
		}
	}
	return result
}
func safeTraceText(value string) string {
	value = sanitizeLogMessage(value)
	runes := []rune(value)
	if len(runes) > 256 {
		value = string(runes[:256]) + "…"
	}
	return value
}

func serveJudgeTraces(ctx context.Context, httpCtx khttp.Context, gateway *service.GatewayService) error {
	rc, ok := gatewaymw.RequestContextFromContext(ctx)
	if !ok || !platformAdministrator(rc.GetRoles()) {
		return kerrors.Forbidden("GATEWAY_OBSERVABILITY_FORBIDDEN", "Judge traces require platform administrator")
	}
	q := httpCtx.Request().URL.Query()
	for key, values := range q {
		if (key != "window" && key != "submission_id") || len(values) != 1 {
			return kerrors.BadRequest("GATEWAY_OBSERVABILITY_INVALID_ARGUMENT", "Unsupported trace filter")
		}
	}
	duration := time.Hour
	switch q.Get("window") {
	case "", "1h":
	case "15m":
		duration = 15 * time.Minute
	case "24h":
		duration = 24 * time.Hour
	default:
		return kerrors.BadRequest("GATEWAY_OBSERVABILITY_INVALID_ARGUMENT", "Unknown time window")
	}
	query := `{ resource.service.name =~ "judge-service|judge-worker|contest-service" && span.submission.id != nil }`
	if raw := q.Get("submission_id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			return kerrors.BadRequest("GATEWAY_OBSERVABILITY_INVALID_ARGUMENT", "Invalid submission ID")
		}
		owner := gateway.SubmissionClient()
		if owner == nil {
			return kerrors.ServiceUnavailable("GATEWAY_OBSERVABILITY_UNAVAILABLE", "Judge is not configured")
		}
		lookup, err := owner.GetSubmission(ctx, &submissionv1.GetSubmissionRequest{SubmissionId: id})
		if err != nil {
			return err
		}
		if lookup == nil || lookup.GetSubmission() == nil || lookup.GetSubmission().GetId() != id {
			return kerrors.New(http.StatusBadGateway, "GATEWAY_OBSERVABILITY_INVALID_JUDGE_RESPONSE", "Invalid submission")
		}
		query = `{ resource.service.name =~ "judge-service|judge-worker|contest-service" && span.submission.id = ` + strconv.FormatInt(id, 10) + ` }`
	}
	result := traceSearchResponse{State: "not_configured", Traces: []traceSummary{}, Limit: 50}
	if _, err := parseLokiURL(os.Getenv("OBSERVABILITY_TEMPO_URL")); err != nil {
		return writeTraceJSON(httpCtx, result)
	}
	end := time.Now().UTC()
	body, _, err := requestTempo(ctx, "/api/search", url.Values{"q": {query}, "start": {strconv.FormatInt(end.Add(-duration).Unix(), 10)}, "end": {strconv.FormatInt(end.Unix(), 10)}, "limit": {"50"}}, "application/json")
	if err != nil {
		result.State, result.ErrorCode = "unavailable", "GATEWAY_TEMPO_UNAVAILABLE"
		return writeTraceJSON(httpCtx, result)
	}
	var payload struct {
		Traces []struct {
			TraceID           string          `json:"traceID"`
			RootServiceName   string          `json:"rootServiceName"`
			RootTraceName     string          `json:"rootTraceName"`
			StartTimeUnixNano string          `json:"startTimeUnixNano"`
			DurationMS        json.RawMessage `json:"durationMs"`
		} `json:"traces"`
	}
	if json.Unmarshal(body, &payload) != nil {
		result.State, result.ErrorCode = "unavailable", "GATEWAY_TEMPO_INVALID_RESPONSE"
		return writeTraceJSON(httpCtx, result)
	}
	seen := map[string]bool{}
	for _, trace := range payload.Traces {
		start, err := strconv.ParseInt(trace.StartTimeUnixNano, 10, 64)
		duration, durationErr := parseTempoDuration(trace.DurationMS)
		if err != nil || durationErr != nil || start <= 0 || !traceIDPattern.MatchString(trace.TraceID) || trace.TraceID == strings.Repeat("0", 32) || seen[trace.TraceID] || duration < 0 {
			continue
		}
		seen[trace.TraceID] = true
		serviceName := trace.RootServiceName
		if _, ok := allowedMetricServices[serviceName]; !ok {
			serviceName = "other"
		}
		result.Traces = append(result.Traces, traceSummary{TraceID: trace.TraceID, Service: serviceName, Name: safeTraceText(trace.RootTraceName), StartedAt: time.Unix(0, start).UTC(), DurationMS: duration})
		if len(result.Traces) == result.Limit {
			break
		}
	}
	sort.Slice(result.Traces, func(i, j int) bool {
		if result.Traces[i].StartedAt.Equal(result.Traces[j].StartedAt) {
			return result.Traces[i].TraceID < result.Traces[j].TraceID
		}
		return result.Traces[i].StartedAt.After(result.Traces[j].StartedAt)
	})
	result.State = "no_data"
	if len(result.Traces) > 0 {
		result.State = "available"
	}
	return writeTraceJSON(httpCtx, result)
}

func parseTempoDuration(raw json.RawMessage) (float64, error) {
	if len(raw) == 0 {
		return 0, fmt.Errorf("missing duration")
	}
	var numeric float64
	if err := json.Unmarshal(raw, &numeric); err == nil {
		return numeric, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return 0, err
	}
	return strconv.ParseFloat(text, 64)
}

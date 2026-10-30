package server

import (
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/viggggil/go_oj_agent/services/gateway/internal/middleware"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/service"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

func avString(value string) *commonpb.AnyValue {
	return &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: value}}
}
func avInt(value int64) *commonpb.AnyValue {
	return &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: value}}
}

func TestProjectTraceFiltersScopeAndSensitiveAttributes(t *testing.T) {
	traceID := strings.Repeat("1", 32)
	traceBytes, _ := hex.DecodeString(traceID)
	root, _ := hex.DecodeString(strings.Repeat("2", 16))
	child, _ := hex.DecodeString(strings.Repeat("3", 16))
	payload := &tracepb.TracesData{ResourceSpans: []*tracepb.ResourceSpans{
		{Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{{Key: "service.name", Value: avString("judge-service")}}}, ScopeSpans: []*tracepb.ScopeSpans{{Spans: []*tracepb.Span{{TraceId: traceBytes, SpanId: root, Name: "judge.root", StartTimeUnixNano: 10, EndTimeUnixNano: 20, Attributes: []*commonpb.KeyValue{{Key: "submission.id", Value: avInt(42)}, {Key: "prompt", Value: avString("private")}}}, {TraceId: traceBytes, SpanId: child, ParentSpanId: root, Name: "judge.worker", StartTimeUnixNano: 20, EndTimeUnixNano: 40}}}}},
		{Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{{Key: "service.name", Value: avString("agent-service")}}}, ScopeSpans: []*tracepb.ScopeSpans{{Spans: []*tracepb.Span{{TraceId: traceBytes, SpanId: []byte{0, 0, 0, 0, 0, 0, 0, 4}, Name: "agent.run", StartTimeUnixNano: 10, EndTimeUnixNano: 20}}}}},
	}}
	agent, err := projectTrace(payload, traceID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(agent) != 1 || agent[0].Service != "agent-service" {
		t.Fatalf("agent scope exposed non-agent spans: %+v", agent)
	}
	system, err := projectTrace(payload, traceID, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(system) != 3 {
		t.Fatalf("projected spans=%+v", system)
	}
	var rootSpan, childSpan traceSpan
	for _, span := range system {
		if span.Name == "judge.root" {
			rootSpan = span
		}
		if span.Name == "judge.worker" {
			childSpan = span
		}
	}
	if childSpan.ParentSpanID != rootSpan.SpanID {
		t.Fatalf("parent relation lost: root=%+v child=%+v", rootSpan, childSpan)
	}
	if _, ok := rootSpan.Attributes["submission.id"]; !ok {
		t.Fatal("safe submission correlation missing")
	}
	if _, ok := rootSpan.Attributes["prompt"]; ok {
		t.Fatal("private prompt escaped allowlist")
	}
}

func TestTraceDetailRequiresRoleAndConfiguration(t *testing.T) {
	t.Setenv("OBSERVABILITY_TEMPO_URL", "")
	auth, err := middleware.NewAuthMiddleware(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	h := NewHTTPServer(testConfig(), auth, service.NewGatewayService(nil, nil))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/observability/traces/11111111111111111111111111111111", nil)
	req.Header.Set("Authorization", "Bearer "+testAccessTokenRoles(t, []string{"agent_admin"}))
	response := httptest.NewRecorder()
	h.ServeHTTP(response, req)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"state":"not_configured"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/observability/traces/11111111111111111111111111111111", nil)
	req.Header.Set("Authorization", "Bearer "+testAccessTokenRoles(t, []string{"user"}))
	response = httptest.NewRecorder()
	h.ServeHTTP(response, req)
	if response.Code != http.StatusForbidden {
		t.Fatalf("user status=%d", response.Code)
	}
}

func TestTempoTraceEndpointProjectsResponse(t *testing.T) {
	traceID := strings.Repeat("a", 32)
	traceBytes, _ := hex.DecodeString(traceID)
	spanID, _ := hex.DecodeString(strings.Repeat("b", 16))
	payload := &tracepb.TracesData{ResourceSpans: []*tracepb.ResourceSpans{{
		Resource:   &resourcepb.Resource{Attributes: []*commonpb.KeyValue{{Key: "service.name", Value: avString("agent-service")}}},
		ScopeSpans: []*tracepb.ScopeSpans{{Spans: []*tracepb.Span{{TraceId: traceBytes, SpanId: spanID, Name: "agent.run", StartTimeUnixNano: 1000000000, EndTimeUnixNano: 2000000000}}}},
	}}}
	data, err := proto.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/traces/"+traceID {
			t.Errorf("path=%s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}))
	defer upstream.Close()
	t.Setenv("OBSERVABILITY_TEMPO_URL", upstream.URL)
	auth, _ := middleware.NewAuthMiddleware(testConfig())
	h := NewHTTPServer(testConfig(), auth, service.NewGatewayService(nil, nil))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/observability/traces/"+traceID, nil)
	req.Header.Set("Authorization", "Bearer "+testAccessTokenRoles(t, []string{"agent_admin"}))
	response := httptest.NewRecorder()
	h.ServeHTTP(response, req)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"name":"agent.run"`) || !strings.Contains(response.Body.String(), `"state":"available"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

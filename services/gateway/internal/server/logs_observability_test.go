package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/viggggil/go_oj_agent/services/gateway/internal/middleware"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/service"
)

func TestBuildLokiQueryUsesAllowlistedFilters(t *testing.T) {
	query := buildLokiQuery("agent-service", "ERROR", "AGENT_RUN_FAILED", "4bf92f3577b34da6a3ce929d0e0e4736", false)
	for _, want := range []string{`service_name="agent-service"`, `level="ERROR"`, `error_code="AGENT_RUN_FAILED"`, `trace_id="4bf92f3577b34da6a3ce929d0e0e4736"`, `| json`} {
		if !strings.Contains(query, want) {
			t.Fatalf("query %q missing %q", query, want)
		}
	}
	if strings.Contains(query, "query=") {
		t.Fatalf("raw query parameter escaped into LogQL: %q", query)
	}
}

func TestLogsEndpointRequiresRoleAndNeverAcceptsLogQL(t *testing.T) {
	auth, err := middleware.NewAuthMiddleware(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHTTPServer(testConfig(), auth, service.NewGatewayService(nil, nil))
	request := httptest.NewRequest(http.MethodGet, observabilityLogsPath+"?query={foo=bar}", nil)
	request.Header.Set("Authorization", "Bearer "+testAccessTokenRoles(t, []string{"user"}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestLogsEndpointUsesFixedQueryAndProjectsSanitizedFields(t *testing.T) {
	var gotQuery string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("query")
		if r.URL.Path != "/loki/api/v1/query_range" {
			t.Errorf("upstream path=%s", r.URL.Path)
		}
		if r.URL.Query().Get("limit") != "20" {
			t.Errorf("limit=%s", r.URL.Query().Get("limit"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"result":[{"stream":{"service_name":"agent-service","internal":"do-not-return"},"values":[["1790000000000000000","{\"timestamp\":\"2026-10-29T00:00:00Z\",\"level\":\"ERROR\",\"service_name\":\"agent-service\",\"message\":\"request failed token=private-token\",\"error_code\":\"AGENT_RUN_FAILED\",\"trace_id\":\"4bf92f3577b34da6a3ce929d0e0e4736\",\"span_id\":\"00f067aa0ba902b7\",\"run_id\":\"run-1\"}"]] }]}}`))
	}))
	defer upstream.Close()
	t.Setenv("OBSERVABILITY_LOKI_URL", upstream.URL)
	auth, err := middleware.NewAuthMiddleware(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHTTPServer(testConfig(), auth, service.NewGatewayService(nil, nil))
	request := httptest.NewRequest(http.MethodGet, observabilityLogsPath+"?service=agent-service&level=ERROR&trace_id=4bf92f3577b34da6a3ce929d0e0e4736&limit=20", nil)
	request.Header.Set("Authorization", "Bearer "+testAccessTokenRoles(t, []string{"agent_admin"}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(gotQuery, "gateway-service") || !strings.Contains(gotQuery, `service_name="agent-service"`) {
		t.Fatalf("query escaped agent scope: %s", gotQuery)
	}
	var result observabilityLogsResponse
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.State != "available" || len(result.Logs) != 1 {
		t.Fatalf("response=%+v", result)
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), "private-token") || strings.Contains(string(encoded), "internal") {
		t.Fatalf("unprojected/sensitive data escaped: %s", encoded)
	}
	if !strings.Contains(string(encoded), `"run_id":"run-1"`) {
		t.Fatalf("allowed business correlation field missing: %s", encoded)
	}
	if !strings.Contains(string(encoded), "[REDACTED]") {
		t.Fatalf("secret not redacted: %s", encoded)
	}
}

func TestLogsEndpointRejectsInvalidTraceAndRange(t *testing.T) {
	for _, suffix := range []string{"?trace_id=not-a-trace", "?start=2026-10-28T00:00:00Z&end=2026-10-30T00:00:00Z", "?limit=201", "?foo=bar"} {
		t.Run(suffix, func(t *testing.T) {
			auth, _ := middleware.NewAuthMiddleware(testConfig())
			handler := NewHTTPServer(testConfig(), auth, service.NewGatewayService(nil, nil))
			request := httptest.NewRequest(http.MethodGet, observabilityLogsPath+suffix, nil)
			request.Header.Set("Authorization", "Bearer "+testAccessTokenRoles(t, []string{"system_admin"}))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/viggggil/go_oj_agent/services/gateway/internal/middleware"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/service"
)

func TestJudgeTraceResponseUsesOnlyConfiguredGrafana(t *testing.T) {
	response := buildJudgeTraceResponse(42, "https://grafana.example/ops/")
	if !response.LinkAvailable || response.TraceAvailable || response.Status != "SEARCHABLE" {
		t.Fatalf("response = %+v, want searchable link without trace existence claim", response)
	}
	if !strings.Contains(response.TempoURL, "grafana.example/ops/explore") || !strings.Contains(response.TempoURL, "submission.id+%3D+42") {
		t.Fatalf("tempo URL = %q, missing fixed Explore query", response.TempoURL)
	}
	parsed, err := url.Parse(response.TempoURL)
	if err != nil {
		t.Fatal(err)
	}
	var left struct {
		Queries []struct {
			QueryType string `json:"queryType"`
			Query     string `json:"query"`
		} `json:"queries"`
	}
	if err := json.Unmarshal([]byte(parsed.Query().Get("left")), &left); err != nil || len(left.Queries) != 1 || left.Queries[0].QueryType != "traceql" || !strings.Contains(left.Queries[0].Query, "span.submission.id = 42") {
		t.Fatalf("left query = %+v, err=%v", left, err)
	}
	if got := buildJudgeTraceResponse(42, "https://grafana.example/#fragment"); !got.LinkAvailable || strings.Contains(got.TempoURL, "#fragment") {
		t.Fatalf("fragment was not removed: %+v", got)
	}
	for _, configured := range []string{"javascript:alert(1)", "https://user:pass@grafana.example", "not a URL"} {
		if got := buildJudgeTraceResponse(42, configured); got.LinkAvailable || got.TempoURL != "" || got.Status != "NOT_CONFIGURED" {
			t.Fatalf("unsafe URL %q accepted: %+v", configured, got)
		}
	}
}

func TestJudgeTraceRouteRequiresPlatformAdministrator(t *testing.T) {
	if platformAdministrator([]string{"agent_admin"}) {
		t.Fatal("agent_admin must not access Judge traces")
	}
	if !platformAdministrator([]string{"system_admin"}) {
		t.Fatal("system_admin must access Judge traces")
	}
}

func TestJudgeTraceHTTPRoute(t *testing.T) {
	t.Setenv("JUDGE_OBSERVABILITY_GRAFANA_URL", "http://localhost:3000")
	auth, err := middleware.NewAuthMiddleware(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	client := &httpSubmissionClient{}
	h := NewHTTPServer(testConfig(), auth, service.NewGatewayServiceWithClients(nil, nil, nil, client))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/observability/judge/submissions/9", nil)
	request.Header.Set("Authorization", "Bearer "+testAccessToken(t))
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"submission_id":9`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if client.getSubmission == nil || client.getSubmission.GetSubmissionId() != 9 {
		t.Fatalf("Judge authorization lookup = %+v", client.getSubmission)
	}

	for _, roles := range [][]string{{"agent_admin"}, {"user"}} {
		request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/observability/judge/submissions/9", nil)
		request.Header.Set("Authorization", "Bearer "+testAccessTokenRoles(t, roles))
		response = httptest.NewRecorder()
		h.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("roles=%v status=%d body=%s, want forbidden", roles, response.Code, response.Body.String())
		}
	}
	request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/observability/judge/submissions/9", nil)
	response = httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status=%d body=%s, want unauthorized", response.Code, response.Body.String())
	}
}

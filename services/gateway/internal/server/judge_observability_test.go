package server

import (
	"net/http"
	"net/http/httptest"
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
	if !strings.Contains(response.TempoURL, "grafana.example/ops/explore") || !strings.Contains(response.TempoURL, "submission.id") {
		t.Fatalf("tempo URL = %q, missing fixed Explore query", response.TempoURL)
	}
	if got := buildJudgeTraceResponse(42, "javascript:alert(1)"); got.LinkAvailable || got.TempoURL != "" || got.Status != "NOT_CONFIGURED" {
		t.Fatalf("unsafe URL accepted: %+v", got)
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

	request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/observability/judge/submissions/9", nil)
	request.Header.Set("Authorization", "Bearer "+testAccessTokenRoles(t, []string{"agent_admin"}))
	response = httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("agent_admin status=%d body=%s, want forbidden", response.Code, response.Body.String())
	}
}

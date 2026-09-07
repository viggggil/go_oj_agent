package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/viggggil/go_oj_agent/services/gateway/internal/conf"
	gatewaymw "github.com/viggggil/go_oj_agent/services/gateway/internal/middleware"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/service"
)

func TestHTTPServerHealthz(t *testing.T) {
	authMiddleware, err := gatewaymw.NewAuthMiddleware(testConfig())
	if err != nil {
		t.Fatalf("NewAuthMiddleware() error = %v", err)
	}
	server := NewHTTPServer(
		testConfig(),
		authMiddleware,
		service.NewAuthService(),
		service.NewUserService(),
		service.NewProblemService(),
		service.NewSubmissionService(),
	)

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	request.Header.Set("X-Request-ID", "req-test")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var body struct {
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
		RequestID string `json:"request_id"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.Status != "ok" || body.RequestID != "req-test" {
		t.Fatalf("body = %#v, want ok with req-test", body)
	}
}

func testConfig() *conf.Bootstrap {
	return &conf.Bootstrap{
		Service: &conf.ServiceProto{Name: "gateway-service"},
		Server:  &conf.ServerProto{Http: &conf.HTTPProto{Address: ":0", Timeout: "3s"}},
		Auth: &conf.AuthProto{
			AccessTokenKey: "test-secret",
			Issuer:         "go-oj-agent",
			Audience:       "go-oj-gateway",
		},
	}
}

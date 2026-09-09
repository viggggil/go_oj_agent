package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	userv1 "github.com/viggggil/go_oj_agent/api/user/v1"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/conf"
	gatewaymw "github.com/viggggil/go_oj_agent/services/gateway/internal/middleware"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/service"
	"google.golang.org/grpc"
)

func TestHTTPServerHealthz(t *testing.T) {
	authMiddleware, err := gatewaymw.NewAuthMiddleware(testConfig())
	if err != nil {
		t.Fatalf("NewAuthMiddleware() error = %v", err)
	}
	server := NewHTTPServer(
		testConfig(),
		authMiddleware,
		service.NewAuthService(&fakeUserClient{}),
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

func TestHTTPServerRegisterForwardsToUserService(t *testing.T) {
	userClient := &fakeUserClient{
		registerResponse: &userv1.RegisterResponse{
			User: &userv1.User{
				Id:       1001,
				Username: "alice",
				Email:    "alice@example.com",
				Status:   "active",
				Roles:    []string{"user"},
			},
		},
	}
	server := newTestHTTPServer(t, userClient)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(`{
		"username": "alice",
		"email": "alice@example.com",
		"password": "correct1"
	}`))
	request.Header.Set("X-Request-ID", "req-register")
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if userClient.registerRequest.GetUsername() != "alice" {
		t.Fatalf("forwarded username = %q, want alice", userClient.registerRequest.GetUsername())
	}
	var body struct {
		Data struct {
			User struct {
				ID       int64    `json:"id"`
				Username string   `json:"username"`
				Email    string   `json:"email"`
				Status   string   `json:"status"`
				Roles    []string `json:"roles"`
			} `json:"user"`
		} `json:"data"`
		RequestID string `json:"request_id"`
	}
	decodeResponse(t, response.Body, &body)
	if body.Data.User.ID != 1001 || body.Data.User.Username != "alice" || body.RequestID != "req-register" {
		t.Fatalf("body = %#v, want registered alice with request id", body)
	}
}

func TestHTTPServerLoginMapsUserServiceError(t *testing.T) {
	userClient := &fakeUserClient{
		loginError: userv1.ErrorUserErrorReasonInvalidCredential("认证凭据无效"),
	}
	server := newTestHTTPServer(t, userClient)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{
		"account": "alice",
		"password": "wrong"
	}`))
	request.Header.Set("X-Request-ID", "req-login")
	server.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var body service.ErrorEnvelope
	decodeResponse(t, response.Body, &body)
	if body.Code != userv1.UserErrorReason_USER_ERROR_REASON_INVALID_CREDENTIAL.String() || body.RequestID != "req-login" {
		t.Fatalf("body = %#v, want invalid credential with request id", body)
	}
}

func TestHTTPServerRegisterRejectsInvalidRequest(t *testing.T) {
	server := newTestHTTPServer(t, &fakeUserClient{})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(`{
		"username": "alice",
		"email": "not-an-email",
		"password": "short"
	}`))
	request.Header.Set("X-Request-ID", "req-invalid")
	server.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var body service.ErrorEnvelope
	decodeResponse(t, response.Body, &body)
	if body.Code != "GATEWAY_INVALID_ARGUMENT" || body.RequestID != "req-invalid" {
		t.Fatalf("body = %#v, want gateway invalid argument", body)
	}
}

func newTestHTTPServer(t *testing.T, userClient userv1.UserServiceClient) http.Handler {
	t.Helper()
	authMiddleware, err := gatewaymw.NewAuthMiddleware(testConfig())
	if err != nil {
		t.Fatalf("NewAuthMiddleware() error = %v", err)
	}
	return NewHTTPServer(
		testConfig(),
		authMiddleware,
		service.NewAuthService(userClient),
		service.NewUserService(),
		service.NewProblemService(),
		service.NewSubmissionService(),
	)
}

func decodeResponse(t *testing.T, body io.Reader, target any) {
	t.Helper()
	if err := json.NewDecoder(body).Decode(target); err != nil {
		t.Fatalf("decode response: %v", err)
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

type fakeUserClient struct {
	registerRequest  *userv1.RegisterRequest
	registerResponse *userv1.RegisterResponse
	registerError    error
	loginRequest     *userv1.LoginRequest
	loginResponse    *userv1.LoginResponse
	loginError       error
	refreshRequest   *userv1.RefreshTokenRequest
	refreshResponse  *userv1.RefreshTokenResponse
	refreshError     error
}

func (c *fakeUserClient) Register(_ context.Context, req *userv1.RegisterRequest, _ ...grpc.CallOption) (*userv1.RegisterResponse, error) {
	c.registerRequest = req
	if c.registerError != nil {
		return nil, c.registerError
	}
	if c.registerResponse != nil {
		return c.registerResponse, nil
	}
	return &userv1.RegisterResponse{}, nil
}

func (c *fakeUserClient) Login(_ context.Context, req *userv1.LoginRequest, _ ...grpc.CallOption) (*userv1.LoginResponse, error) {
	c.loginRequest = req
	if c.loginError != nil {
		return nil, c.loginError
	}
	if c.loginResponse != nil {
		return c.loginResponse, nil
	}
	return &userv1.LoginResponse{}, nil
}

func (c *fakeUserClient) RefreshToken(_ context.Context, req *userv1.RefreshTokenRequest, _ ...grpc.CallOption) (*userv1.RefreshTokenResponse, error) {
	c.refreshRequest = req
	if c.refreshError != nil {
		return nil, c.refreshError
	}
	if c.refreshResponse != nil {
		return c.refreshResponse, nil
	}
	return &userv1.RefreshTokenResponse{}, nil
}

func (*fakeUserClient) GetCurrentUser(context.Context, *userv1.GetCurrentUserRequest, ...grpc.CallOption) (*userv1.GetCurrentUserResponse, error) {
	return nil, userv1.ErrorUserErrorReasonPermissionDenied("未接入当前用户接口")
}

func (*fakeUserClient) GetUser(context.Context, *userv1.GetUserRequest, ...grpc.CallOption) (*userv1.GetUserResponse, error) {
	return nil, userv1.ErrorUserErrorReasonPermissionDenied("未接入用户查询接口")
}

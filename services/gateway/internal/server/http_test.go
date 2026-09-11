package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
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
		service.NewGatewayService(
			service.NewAuthService(&fakeUserClient{}),
			service.NewUserService(&fakeUserClient{}),
		),
	)

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	request.Header.Set("X-Request-ID", "req-test")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != "ok" || response.Header().Get("X-Request-ID") != "req-test" {
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
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-ID", "req-register")
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if userClient.registerRequest.GetUsername() != "alice" {
		t.Fatalf("forwarded username = %q, want alice", userClient.registerRequest.GetUsername())
	}
	var body struct {
		User struct {
			ID       int64    `json:"id"`
			Username string   `json:"username"`
			Email    string   `json:"email"`
			Status   string   `json:"status"`
			Roles    []string `json:"roles"`
		} `json:"user"`
	}
	decodeResponse(t, response.Body, &body)
	if body.User.ID != 1001 || body.User.Username != "alice" || response.Header().Get("X-Request-ID") != "req-register" {
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
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-ID", "req-login")
	server.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var body kratosErrorResponse
	decodeResponse(t, response.Body, &body)
	if body.Reason != userv1.UserErrorReason_USER_ERROR_REASON_INVALID_CREDENTIAL.String() || response.Header().Get("X-Request-ID") != "req-login" {
		t.Fatalf("body = %#v, want invalid credential with request id", body)
	}
}

func TestHTTPServerRegisterRejectsInvalidRequest(t *testing.T) {
	userClient := &fakeUserClient{}
	server := newTestHTTPServer(t, userClient)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(`{
		"username": "alice",
		"email": "not-an-email",
		"password": "short"
	}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-ID", "req-invalid")
	server.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var body kratosErrorResponse
	decodeResponse(t, response.Body, &body)
	if body.Reason != "VALIDATOR" || response.Header().Get("X-Request-ID") != "req-invalid" {
		t.Fatalf("body = %#v, want validator error", body)
	}
	if userClient.registerRequest != nil {
		t.Fatal("参数校验失败时不应调用 user-service")
	}
}

func TestHTTPServerProtectedUserRequiresToken(t *testing.T) {
	server := newTestHTTPServer(t, &fakeUserClient{})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
	request.Header.Set("X-Request-ID", "req-me")
	server.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var body kratosErrorResponse
	decodeResponse(t, response.Body, &body)
	if body.Reason != "GATEWAY_UNAUTHENTICATED" || response.Header().Get("X-Request-ID") != "req-me" {
		t.Fatalf("body = %#v, want unauthenticated error", body)
	}
}

func TestHTTPServerCurrentUserForwardsAuthenticatedContext(t *testing.T) {
	userClient := &fakeUserClient{
		currentUserResponse: &userv1.GetCurrentUserResponse{
			User: &userv1.User{Id: 1001, Username: "alice", Status: "active", Roles: []string{"user"}},
		},
	}
	server := newTestHTTPServer(t, userClient)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
	request.Header.Set("Authorization", "Bearer "+testAccessToken(t))
	request.Header.Set("X-Request-ID", "req-me")
	request.Header.Set("X-Trace-ID", "trace-me")
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if got := userClient.currentUserRequest.GetContext().GetUserId(); got != 1001 {
		t.Fatalf("user id = %d, want 1001", got)
	}
	if got := userClient.currentUserRequest.GetContext().GetRequestId(); got != "req-me" {
		t.Fatalf("request id = %q, want req-me", got)
	}
	if got := userClient.currentUserRequest.GetContext().GetTraceId(); got != "trace-me" {
		t.Fatalf("trace id = %q, want trace-me", got)
	}
	if got := userClient.currentUserRequest.GetContext().GetRoles(); len(got) != 2 || got[0] != "user" || got[1] != "admin" {
		t.Fatalf("roles = %#v, want user/admin", got)
	}
}

func TestHTTPServerGetUserForwardsPathIDAndContext(t *testing.T) {
	userClient := &fakeUserClient{
		userResponse: &userv1.GetUserResponse{
			User: &userv1.User{Id: 1002, Username: "bob", Status: "active", Roles: []string{"user"}},
		},
	}
	server := newTestHTTPServer(t, userClient)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/users/1002", nil)
	request.Header.Set("Authorization", "Bearer "+testAccessToken(t))
	request.Header.Set("X-Request-ID", "req-user")
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if userClient.userRequest.GetUserId() != 1002 {
		t.Fatalf("user id = %d, want 1002", userClient.userRequest.GetUserId())
	}
	if userClient.userRequest.GetContext().GetUserId() != 1001 {
		t.Fatalf("requester id = %d, want 1001", userClient.userRequest.GetContext().GetUserId())
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
		service.NewGatewayService(
			service.NewAuthService(userClient),
			service.NewUserService(userClient),
		),
	)
}

type kratosErrorResponse struct {
	Code    int32  `json:"code"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
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
	registerRequest     *userv1.RegisterRequest
	registerResponse    *userv1.RegisterResponse
	registerError       error
	loginRequest        *userv1.LoginRequest
	loginResponse       *userv1.LoginResponse
	loginError          error
	refreshRequest      *userv1.RefreshTokenRequest
	refreshResponse     *userv1.RefreshTokenResponse
	refreshError        error
	currentUserRequest  *userv1.GetCurrentUserRequest
	currentUserResponse *userv1.GetCurrentUserResponse
	currentUserError    error
	userRequest         *userv1.GetUserRequest
	userResponse        *userv1.GetUserResponse
	userError           error
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

func (*fakeUserClient) Logout(context.Context, *userv1.LogoutRequest, ...grpc.CallOption) (*userv1.LogoutResponse, error) {
	return &userv1.LogoutResponse{}, nil
}

func (c *fakeUserClient) GetCurrentUser(_ context.Context, req *userv1.GetCurrentUserRequest, _ ...grpc.CallOption) (*userv1.GetCurrentUserResponse, error) {
	c.currentUserRequest = req
	if c.currentUserError != nil {
		return nil, c.currentUserError
	}
	if c.currentUserResponse != nil {
		return c.currentUserResponse, nil
	}
	return &userv1.GetCurrentUserResponse{}, nil
}

func (c *fakeUserClient) GetUser(_ context.Context, req *userv1.GetUserRequest, _ ...grpc.CallOption) (*userv1.GetUserResponse, error) {
	c.userRequest = req
	if c.userError != nil {
		return nil, c.userError
	}
	if c.userResponse != nil {
		return c.userResponse, nil
	}
	return &userv1.GetUserResponse{}, nil
}

func testAccessToken(t *testing.T) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	claims := `{"sub":1001,"username":"alice","roles":["user","admin"],"iss":"go-oj-agent","aud":"go-oj-gateway","iat":4102444800,"exp":4102448400,"jti":"test-token"}`
	payload := base64.RawURLEncoding.EncodeToString([]byte(claims))
	signed := header + "." + payload
	mac := hmac.New(sha256.New, []byte("test-secret"))
	_, _ = mac.Write([]byte(signed))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return signed + "." + signature
}

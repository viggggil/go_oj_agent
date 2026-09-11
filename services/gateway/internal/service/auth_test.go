package service

import (
	"context"
	"testing"

	gatewayv1 "github.com/viggggil/go_oj_agent/api/gateway/v1"
	userv1 "github.com/viggggil/go_oj_agent/api/user/v1"
	"google.golang.org/grpc"
)

func TestAuthServiceRegisterForwardsRequest(t *testing.T) {
	client := &fakeUserServiceClient{
		registerResponse: &userv1.RegisterResponse{
			User: &userv1.User{Id: 1001, Username: "alice", Email: "alice@example.com", Status: "active", Roles: []string{"user"}},
		},
	}
	resp, err := NewAuthService(client).Register(context.Background(), &gatewayv1.RegisterRequest{
		Username: "alice",
		Email:    "alice@example.com",
		Password: "correct1",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if client.registerRequest.GetUsername() != "alice" || client.registerRequest.GetPassword() != "correct1" {
		t.Fatalf("forwarded request = %#v", client.registerRequest)
	}
	if resp.GetUser().GetId() != 1001 || resp.GetUser().GetEmail() != "alice@example.com" {
		t.Fatalf("response = %#v", resp)
	}
}

func TestAuthServiceLoginForwardsRequest(t *testing.T) {
	client := &fakeUserServiceClient{
		loginResponse: &userv1.LoginResponse{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 900},
	}
	resp, err := NewAuthService(client).Login(context.Background(), &gatewayv1.LoginRequest{
		Account:  "alice",
		Password: "correct1",
	})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if client.loginRequest.GetAccount() != "alice" || client.loginRequest.GetPassword() != "correct1" {
		t.Fatalf("forwarded request = %#v", client.loginRequest)
	}
	if resp.GetAccessToken() != "access" || resp.GetRefreshToken() != "refresh" || resp.GetExpiresIn() != 900 {
		t.Fatalf("response = %#v", resp)
	}
}

func TestAuthServiceRefreshTokenForwardsRequest(t *testing.T) {
	client := &fakeUserServiceClient{
		refreshResponse: &userv1.RefreshTokenResponse{AccessToken: "next-access", RefreshToken: "next-refresh", ExpiresIn: 900},
	}
	resp, err := NewAuthService(client).RefreshToken(context.Background(), &gatewayv1.RefreshTokenRequest{
		RefreshToken: "old-refresh",
	})
	if err != nil {
		t.Fatalf("RefreshToken() error = %v", err)
	}
	if client.refreshRequest.GetRefreshToken() != "old-refresh" {
		t.Fatalf("forwarded request = %#v", client.refreshRequest)
	}
	if resp.GetAccessToken() != "next-access" || resp.GetRefreshToken() != "next-refresh" {
		t.Fatalf("response = %#v", resp)
	}
}

func TestAuthServiceReturnsUserServiceError(t *testing.T) {
	want := userv1.ErrorUserErrorReasonInvalidCredential("认证凭据无效")
	client := &fakeUserServiceClient{loginError: want}
	_, err := NewAuthService(client).Login(context.Background(), &gatewayv1.LoginRequest{
		Account:  "alice",
		Password: "wrong",
	})
	if err != want {
		t.Fatalf("Login() error = %v, want %v", err, want)
	}
}

type fakeUserServiceClient struct {
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

func (c *fakeUserServiceClient) Register(_ context.Context, req *userv1.RegisterRequest, _ ...grpc.CallOption) (*userv1.RegisterResponse, error) {
	c.registerRequest = req
	if c.registerError != nil {
		return nil, c.registerError
	}
	if c.registerResponse != nil {
		return c.registerResponse, nil
	}
	return &userv1.RegisterResponse{}, nil
}

func (c *fakeUserServiceClient) Login(_ context.Context, req *userv1.LoginRequest, _ ...grpc.CallOption) (*userv1.LoginResponse, error) {
	c.loginRequest = req
	if c.loginError != nil {
		return nil, c.loginError
	}
	if c.loginResponse != nil {
		return c.loginResponse, nil
	}
	return &userv1.LoginResponse{}, nil
}

func (c *fakeUserServiceClient) RefreshToken(_ context.Context, req *userv1.RefreshTokenRequest, _ ...grpc.CallOption) (*userv1.RefreshTokenResponse, error) {
	c.refreshRequest = req
	if c.refreshError != nil {
		return nil, c.refreshError
	}
	if c.refreshResponse != nil {
		return c.refreshResponse, nil
	}
	return &userv1.RefreshTokenResponse{}, nil
}

func (*fakeUserServiceClient) Logout(context.Context, *userv1.LogoutRequest, ...grpc.CallOption) (*userv1.LogoutResponse, error) {
	return &userv1.LogoutResponse{}, nil
}

func (*fakeUserServiceClient) GetCurrentUser(context.Context, *userv1.GetCurrentUserRequest, ...grpc.CallOption) (*userv1.GetCurrentUserResponse, error) {
	return nil, userv1.ErrorUserErrorReasonPermissionDenied("未接入当前用户接口")
}

func (*fakeUserServiceClient) GetUser(context.Context, *userv1.GetUserRequest, ...grpc.CallOption) (*userv1.GetUserResponse, error) {
	return nil, userv1.ErrorUserErrorReasonPermissionDenied("未接入用户查询接口")
}

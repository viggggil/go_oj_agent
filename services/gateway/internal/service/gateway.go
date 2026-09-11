package service

import (
	"context"

	gatewayv1 "github.com/viggggil/go_oj_agent/api/gateway/v1"
)

// GatewayService 实现由 gateway.proto 生成的 HTTP 服务接口。
type GatewayService struct {
	auth *AuthService
	user *UserService
}

func NewGatewayService(auth *AuthService, user *UserService) *GatewayService {
	return &GatewayService{
		auth: auth,
		user: user,
	}
}

func (s *GatewayService) Health(context.Context, *gatewayv1.HealthRequest) (*gatewayv1.HealthResponse, error) {
	return &gatewayv1.HealthResponse{Status: "ok"}, nil
}

func (s *GatewayService) Register(ctx context.Context, req *gatewayv1.RegisterRequest) (*gatewayv1.RegisterResponse, error) {
	return s.auth.Register(ctx, req)
}

func (s *GatewayService) Login(ctx context.Context, req *gatewayv1.LoginRequest) (*gatewayv1.LoginResponse, error) {
	return s.auth.Login(ctx, req)
}

func (s *GatewayService) RefreshToken(ctx context.Context, req *gatewayv1.RefreshTokenRequest) (*gatewayv1.RefreshTokenResponse, error) {
	return s.auth.RefreshToken(ctx, req)
}

func (s *GatewayService) Logout(ctx context.Context, req *gatewayv1.LogoutRequest) (*gatewayv1.LogoutResponse, error) {
	return s.auth.Logout(ctx, req)
}

func (s *GatewayService) GetCurrentUser(ctx context.Context, req *gatewayv1.GetCurrentUserRequest) (*gatewayv1.GetCurrentUserResponse, error) {
	return s.user.GetCurrentUser(ctx, req)
}

func (s *GatewayService) GetUser(ctx context.Context, req *gatewayv1.GetUserRequest) (*gatewayv1.GetUserResponse, error) {
	return s.user.GetUser(ctx, req)
}

var _ gatewayv1.GatewayServiceHTTPServer = (*GatewayService)(nil)

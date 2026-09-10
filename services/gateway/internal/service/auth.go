package service

import (
	"context"
	"fmt"

	"github.com/google/wire"

	gatewayv1 "github.com/viggggil/go_oj_agent/api/gateway/v1"
	userv1 "github.com/viggggil/go_oj_agent/api/user/v1"
)

var ProviderSet = wire.NewSet(
	NewAuthService,
	NewUserService,
	NewGatewayService,
	NewProblemService,
	NewSubmissionService,
)

type AuthService struct {
	users userv1.UserServiceClient
}

func NewAuthService(users userv1.UserServiceClient) *AuthService {
	return &AuthService{users: users}
}

func (s *AuthService) Register(ctx context.Context, req *gatewayv1.RegisterRequest) (*gatewayv1.RegisterResponse, error) {
	if s == nil || s.users == nil || req == nil {
		return nil, fmt.Errorf("gateway auth service is not configured")
	}
	resp, err := s.users.Register(ctx, &userv1.RegisterRequest{
		Username: req.GetUsername(),
		Email:    req.GetEmail(),
		Password: req.GetPassword(),
	})
	if err != nil {
		return nil, err
	}
	return &gatewayv1.RegisterResponse{User: toGatewayUser(resp.GetUser())}, nil
}

func (s *AuthService) Login(ctx context.Context, req *gatewayv1.LoginRequest) (*gatewayv1.LoginResponse, error) {
	if s == nil || s.users == nil || req == nil {
		return nil, fmt.Errorf("gateway auth service is not configured")
	}
	resp, err := s.users.Login(ctx, &userv1.LoginRequest{
		Account:  req.GetAccount(),
		Password: req.GetPassword(),
	})
	if err != nil {
		return nil, err
	}
	return &gatewayv1.LoginResponse{
		AccessToken:  resp.GetAccessToken(),
		RefreshToken: resp.GetRefreshToken(),
		ExpiresIn:    resp.GetExpiresIn(),
	}, nil
}

func (s *AuthService) RefreshToken(ctx context.Context, req *gatewayv1.RefreshTokenRequest) (*gatewayv1.RefreshTokenResponse, error) {
	if s == nil || s.users == nil || req == nil {
		return nil, fmt.Errorf("gateway auth service is not configured")
	}
	resp, err := s.users.RefreshToken(ctx, &userv1.RefreshTokenRequest{
		RefreshToken: req.GetRefreshToken(),
	})
	if err != nil {
		return nil, err
	}
	return &gatewayv1.RefreshTokenResponse{
		AccessToken:  resp.GetAccessToken(),
		RefreshToken: resp.GetRefreshToken(),
		ExpiresIn:    resp.GetExpiresIn(),
	}, nil
}

func toGatewayUser(user *userv1.User) *gatewayv1.UserSummary {
	if user == nil {
		return nil
	}
	return &gatewayv1.UserSummary{
		Id:       user.GetId(),
		Username: user.GetUsername(),
		Email:    user.GetEmail(),
		Status:   user.GetStatus(),
		Roles:    append([]string(nil), user.GetRoles()...),
	}
}

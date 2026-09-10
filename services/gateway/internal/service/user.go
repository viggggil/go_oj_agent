package service

import (
	"context"
	"fmt"

	gatewayv1 "github.com/viggggil/go_oj_agent/api/gateway/v1"
	userv1 "github.com/viggggil/go_oj_agent/api/user/v1"
	gatewaymw "github.com/viggggil/go_oj_agent/services/gateway/internal/middleware"
)

type UserService struct {
	users userv1.UserServiceClient
}

func NewUserService(users userv1.UserServiceClient) *UserService {
	return &UserService{users: users}
}

func (s *UserService) GetCurrentUser(ctx context.Context, _ *gatewayv1.GetCurrentUserRequest) (*gatewayv1.GetCurrentUserResponse, error) {
	requestContext, ok := gatewaymw.RequestContextFromContext(ctx)
	if !ok {
		return nil, gatewaymw.ErrUnauthenticated("request context is missing")
	}
	if s == nil || s.users == nil || requestContext == nil {
		return nil, fmt.Errorf("gateway user service is not configured")
	}
	resp, err := s.users.GetCurrentUser(ctx, &userv1.GetCurrentUserRequest{
		Context: requestContext,
	})
	if err != nil {
		return nil, err
	}
	return &gatewayv1.GetCurrentUserResponse{User: toGatewayUser(resp.GetUser())}, nil
}

func (s *UserService) GetUser(ctx context.Context, req *gatewayv1.GetUserRequest) (*gatewayv1.GetUserResponse, error) {
	requestContext, ok := gatewaymw.RequestContextFromContext(ctx)
	if !ok {
		return nil, gatewaymw.ErrUnauthenticated("request context is missing")
	}
	if req == nil {
		return nil, ErrInvalidRequest(fmt.Errorf("user request is required"))
	}
	if err := req.Validate(); err != nil {
		return nil, ErrInvalidRequest(err)
	}
	if s == nil || s.users == nil || requestContext == nil {
		return nil, fmt.Errorf("gateway user service is not configured")
	}
	resp, err := s.users.GetUser(ctx, &userv1.GetUserRequest{
		Context: requestContext,
		UserId:  req.GetId(),
	})
	if err != nil {
		return nil, err
	}
	return &gatewayv1.GetUserResponse{User: toGatewayUser(resp.GetUser())}, nil
}

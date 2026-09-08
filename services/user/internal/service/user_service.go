package service

import (
	"context"
	"errors"

	"github.com/google/wire"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	userv1 "github.com/viggggil/go_oj_agent/api/user/v1"
	"github.com/viggggil/go_oj_agent/services/user/internal/biz"
)

var ProviderSet = wire.NewSet(NewUserService)

const Name = "user-service"

type UserService struct {
	userv1.UnimplementedUserServiceServer
	uc *biz.UserUsecase
}

func NewUserService(uc *biz.UserUsecase) *UserService {
	return &UserService{
		uc: uc,
	}
}

func (s *UserService) Register(
	ctx context.Context,
	req *userv1.RegisterRequest,
) (*userv1.RegisterResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid register request")
	}
	if err := req.Validate(); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if s == nil || s.uc == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid register request")
	}

	user, err := s.uc.Register(ctx, biz.RegisterInput{
		Username: req.GetUsername(),
		Email:    req.GetEmail(),
		Password: req.GetPassword(),
	})
	if err != nil {
		return nil, toStatusError(err)
	}

	return &userv1.RegisterResponse{
		User: toProtoUser(user),
	}, nil
}

func (s *UserService) Login(
	ctx context.Context,
	req *userv1.LoginRequest,
) (*userv1.LoginResponse, error) {
	if req == nil || s == nil || s.uc == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid login request")
	}
	if err := req.Validate(); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	tokens, err := s.uc.Login(ctx, biz.LoginInput{
		Account:  req.GetAccount(),
		Password: req.GetPassword(),
	})
	if err != nil {
		return nil, toStatusError(err)
	}
	return &userv1.LoginResponse{
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		ExpiresIn:    int64(tokens.ExpiresIn.Seconds()),
	}, nil
}

func (s *UserService) RefreshToken(
	ctx context.Context,
	req *userv1.RefreshTokenRequest,
) (*userv1.RefreshTokenResponse, error) {
	if req == nil || s == nil || s.uc == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid refresh token request")
	}
	if err := req.Validate(); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	tokens, err := s.uc.RefreshToken(ctx, biz.RefreshTokenInput{
		RefreshToken: req.GetRefreshToken(),
	})
	if err != nil {
		return nil, toStatusError(err)
	}
	return &userv1.RefreshTokenResponse{
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		ExpiresIn:    int64(tokens.ExpiresIn.Seconds()),
	}, nil
}

func (s *UserService) GetCurrentUser(
	ctx context.Context,
	req *userv1.GetCurrentUserRequest,
) (*userv1.GetCurrentUserResponse, error) {
	if req == nil || s == nil || s.uc == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid get current user request")
	}
	if err := req.Validate(); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	user, err := s.uc.GetCurrentUser(ctx, biz.NewRequestContext(req.GetContext()))
	if err != nil {
		return nil, toStatusError(err)
	}
	return &userv1.GetCurrentUserResponse{
		User: toProtoUser(user),
	}, nil
}

func (s *UserService) GetUser(
	ctx context.Context,
	req *userv1.GetUserRequest,
) (*userv1.GetUserResponse, error) {
	if req == nil || s == nil || s.uc == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid get user request")
	}
	if err := req.Validate(); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	user, err := s.uc.GetUser(ctx, biz.NewRequestContext(req.GetContext()), req.GetUserId())
	if err != nil {
		return nil, toStatusError(err)
	}
	return &userv1.GetUserResponse{
		User: toProtoUser(user),
	}, nil
}

func toProtoUser(user biz.User) *userv1.User {
	roles := make([]string, 0, len(user.Roles))
	for _, role := range user.Roles {
		roles = append(roles, string(role))
	}
	return &userv1.User{
		Id:       user.ID,
		Username: user.Username,
		Email:    user.Email,
		Status:   string(user.Status),
		Roles:    roles,
	}
}

func toStatusError(err error) error {
	var domainErr *biz.Error
	if errors.As(err, &domainErr) {
		return domainErr
	}
	return status.Error(codes.Internal, err.Error())
}

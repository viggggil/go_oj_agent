package service

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	userv1 "github.com/viggggil/go_oj_agent/api/user/v1"
	"github.com/viggggil/go_oj_agent/services/user/internal/biz"
)

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
		return nil, biz.ErrInvalidArgument
	}
	if s == nil || s.uc == nil {
		return nil, biz.ErrInvalidArgument
	}

	user, err := s.uc.Register(ctx, biz.RegisterInput{
		Username: req.GetUsername(),
		Email:    req.GetEmail(),
		Password: req.GetPassword(),
	})
	if err != nil {
		return nil, err
	}

	return &userv1.RegisterResponse{
		User: toProtoUser(user),
	}, nil
}

func (s *UserService) Login(
	context.Context,
	*userv1.LoginRequest,
) (*userv1.LoginResponse, error) {
	return nil, status.Error(codes.Unimplemented, "Login is not implemented")
}

func (s *UserService) RefreshToken(
	context.Context,
	*userv1.RefreshTokenRequest,
) (*userv1.RefreshTokenResponse, error) {
	return nil, status.Error(codes.Unimplemented, "RefreshToken is not implemented")
}

func (s *UserService) GetCurrentUser(
	context.Context,
	*userv1.GetCurrentUserRequest,
) (*userv1.GetCurrentUserResponse, error) {
	return nil, status.Error(codes.Unimplemented, "GetCurrentUser is not implemented")
}

func (s *UserService) GetUser(
	context.Context,
	*userv1.GetUserRequest,
) (*userv1.GetUserResponse, error) {
	return nil, status.Error(codes.Unimplemented, "GetUser is not implemented")
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

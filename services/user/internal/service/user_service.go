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

func toStatusError(err error) error {
	switch err {
	case biz.ErrInvalidArgument:
		return status.Error(codes.InvalidArgument, err.Error())
	case biz.ErrInvalidCredential:
		return status.Error(codes.Unauthenticated, err.Error())
	case biz.ErrUserAlreadyExists:
		return status.Error(codes.AlreadyExists, err.Error())
	case biz.ErrUserNotFound:
		return status.Error(codes.NotFound, err.Error())
	case biz.ErrUserInactive:
		return status.Error(codes.PermissionDenied, err.Error())
	default:
		return status.Error(codes.Internal, err.Error())
	}
}

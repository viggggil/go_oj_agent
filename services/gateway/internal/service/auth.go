package service

import "github.com/google/wire"

var ProviderSet = wire.NewSet(
	NewAuthService,
	NewUserService,
	NewProblemService,
	NewSubmissionService,
)

type AuthService struct{}

func NewAuthService() *AuthService {
	return &AuthService{}
}

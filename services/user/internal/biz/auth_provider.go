package biz

import (
	"fmt"
	"time"

	"github.com/viggggil/go_oj_agent/services/user/internal/conf"
)

func NewHMACTokenManagerFromConfig(config *conf.Bootstrap) (*HMACTokenManager, error) {
	if config == nil || config.GetAuth() == nil {
		return nil, ErrInvalidArgument
	}
	accessTTL, err := time.ParseDuration(config.GetAuth().GetAccessTokenTtl())
	if err != nil {
		return nil, fmt.Errorf("auth.access_token_ttl is invalid: %w", err)
	}
	refreshTTL, err := time.ParseDuration(config.GetAuth().GetRefreshTokenTtl())
	if err != nil {
		return nil, fmt.Errorf("auth.refresh_token_ttl is invalid: %w", err)
	}
	return NewHMACTokenManager(TokenConfig{
		Secret:          config.GetAuth().GetAccessTokenKey(),
		Issuer:          config.GetAuth().GetIssuer(),
		Audience:        config.GetAuth().GetAudience(),
		AccessTokenTTL:  accessTTL,
		RefreshTokenTTL: refreshTTL,
	})
}

func NewUserUsecaseFromConfig(
	config *conf.Bootstrap,
	users UserRepository,
	roles RoleRepository,
	tokens *HMACTokenManager,
	refreshTokens RefreshTokenStore,
) *UserUsecase {
	if config == nil || config.GetAuth() == nil {
		return NewUserUsecase(UserUsecaseOptions{})
	}
	passwordCfg := config.GetAuth().GetPassword()
	if passwordCfg == nil {
		passwordCfg = &conf.PasswordProto{}
	}
	return NewUserUsecase(UserUsecaseOptions{
		Users:         users,
		Roles:         roles,
		Passwords:     NewBcryptPasswordHasher(int(passwordCfg.GetBcryptCost())),
		Tokens:        tokens,
		RefreshToken:  tokens,
		RefreshTokens: refreshTokens,
		PasswordPolicy: PasswordPolicy{
			MinLength: int(passwordCfg.GetMinLength()),
			MaxBytes:  int(passwordCfg.GetMaxBytes()),
		},
	})
}

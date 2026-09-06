package biz

import "github.com/viggggil/go_oj_agent/services/user/internal/conf"

func NewHMACTokenManagerFromConfig(config *conf.Config) (*HMACTokenManager, error) {
	if config == nil {
		return nil, ErrInvalidArgument
	}
	return NewHMACTokenManager(TokenConfig{
		Secret:          config.Auth.AccessTokenKey,
		Issuer:          config.Auth.Issuer,
		Audience:        config.Auth.Audience,
		AccessTokenTTL:  config.Auth.AccessTokenTTL,
		RefreshTokenTTL: config.Auth.RefreshTokenTTL,
	})
}

func NewUserUsecaseFromConfig(
	config *conf.Config,
	users UserRepository,
	roles RoleRepository,
	tokens *HMACTokenManager,
	refreshTokens RefreshTokenStore,
) *UserUsecase {
	return NewUserUsecase(UserUsecaseOptions{
		Users:         users,
		Roles:         roles,
		Passwords:     NewBcryptPasswordHasher(config.Auth.Password.BcryptCost),
		Tokens:        tokens,
		RefreshToken:  tokens,
		RefreshTokens: refreshTokens,
		PasswordPolicy: PasswordPolicy{
			MinLength: config.Auth.Password.MinLength,
			MaxBytes:  config.Auth.Password.MaxBytes,
		},
	})
}

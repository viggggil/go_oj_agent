package security

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/wire"
	"github.com/viggggil/go_oj_agent/services/user/internal/conf"
)

var (
	ErrInvalidArgument   = errors.New("invalid argument")
	ErrInvalidCredential = errors.New("invalid credential")
)

var ProviderSet = wire.NewSet(NewHMACTokenManagerFromConfig)

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

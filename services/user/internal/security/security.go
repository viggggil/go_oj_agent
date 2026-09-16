package security

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/google/wire"
	"github.com/viggggil/go_oj_agent/services/user/internal/conf"
)

var (
	ErrInvalidArgument   = errors.New("invalid argument")
	ErrInvalidCredential = errors.New("invalid credential")
)

var ProviderSet = wire.NewSet(NewRS256TokenManagerFromConfig)

type TokenManager interface {
	IssueAccessToken(context.Context, TokenSubject) (string, time.Duration, error)
	Generate(int64, string, string) (string, RefreshTokenRecord, error)
	Hash(string) string
}

func NewRS256TokenManagerFromConfig(config *conf.Bootstrap) (TokenManager, error) {
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
	keyPath := config.GetAuth().GetAccessTokenPrivateKeyFile()
	if keyPath == "" {
		// Deprecated compatibility for existing local deployments. New deployments
		// must provide an RSA private key file and key id.
		return NewHMACTokenManager(TokenConfig{Secret: config.GetAuth().GetAccessTokenKey(), Issuer: config.GetAuth().GetIssuer(), Audience: config.GetAuth().GetAudience(), AccessTokenTTL: accessTTL, RefreshTokenTTL: refreshTTL})
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read access token private key: %w", err)
	}
	return NewRS256TokenManager(key, config.GetAuth().GetAccessTokenKeyId(), config.GetAuth().GetIssuer(), config.GetAuth().GetAudience(), accessTTL, refreshTTL, nil)
}

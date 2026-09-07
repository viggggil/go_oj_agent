package middleware

import (
	"fmt"
	"strings"

	pkgauth "github.com/viggggil/go_oj_agent/pkg/auth"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/conf"
)

type AuthMiddleware struct {
	verifier *pkgauth.HMACVerifier
}

func NewAuthMiddleware(config *conf.Bootstrap) (*AuthMiddleware, error) {
	if config == nil || config.GetAuth() == nil {
		return nil, fmt.Errorf("gateway auth config is required")
	}
	verifier, err := pkgauth.NewHMACVerifier(pkgauth.HMACVerifierConfig{
		Secret:   config.GetAuth().GetAccessTokenKey(),
		Issuer:   config.GetAuth().GetIssuer(),
		Audience: config.GetAuth().GetAudience(),
	})
	if err != nil {
		return nil, err
	}
	return &AuthMiddleware{verifier: verifier}, nil
}

func BearerToken(authorization string) (string, bool) {
	const prefix = "Bearer "
	if !strings.HasPrefix(authorization, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(authorization, prefix))
	return token, token != ""
}

package middleware

import (
	"context"
	"fmt"
	"strings"

	kerrors "github.com/go-kratos/kratos/v3/errors"
	"github.com/go-kratos/kratos/v3/middleware"
	khttp "github.com/go-kratos/kratos/v3/transport/http"
	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
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

func (m *AuthMiddleware) Middleware() middleware.Middleware {
	return func(next middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req interface{}) (interface{}, error) {
			if m == nil || m.verifier == nil {
				return nil, ErrUnauthenticated("gateway auth middleware is not configured")
			}
			request, ok := khttp.RequestFromServerContext(ctx)
			if !ok {
				return nil, ErrUnauthenticated("http request is missing")
			}
			token, ok := BearerToken(request.Header.Get("Authorization"))
			if !ok {
				return nil, ErrUnauthenticated("bearer token is required")
			}
			claims, err := m.verifier.Verify(token)
			if err != nil {
				return nil, ErrUnauthenticated("bearer token is invalid")
			}
			requestContext := &commonv1.RequestContext{
				UserId:    claims.Subject,
				Roles:     append([]string(nil), claims.Roles...),
				RequestId: RequestIDFromContext(ctx),
				TraceId:   request.Header.Get("X-Trace-ID"),
			}
			ctx = context.WithValue(ctx, claimsKey, claims)
			ctx = context.WithValue(ctx, requestContextKey, requestContext)
			return next(ctx, req)
		}
	}
}

func ErrUnauthenticated(format string, args ...any) *kerrors.Error {
	return kerrors.New(401, "GATEWAY_UNAUTHENTICATED", fmt.Sprintf(format, args...))
}

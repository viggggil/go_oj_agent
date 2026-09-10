package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	kerrors "github.com/go-kratos/kratos/v3/errors"
	"github.com/go-kratos/kratos/v3/middleware"
	khttp "github.com/go-kratos/kratos/v3/transport/http"
	"github.com/google/wire"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	pkgauth "github.com/viggggil/go_oj_agent/pkg/auth"
)

type contextKey string

const (
	requestIDKey      contextKey = "request_id"
	claimsKey         contextKey = "auth_claims"
	requestContextKey contextKey = "request_context"
)

var ProviderSet = wire.NewSet(NewAuthMiddleware)

func RequestIDMiddleware() middleware.Middleware {
	return func(next middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req interface{}) (interface{}, error) {
			requestID := RequestIDFromContext(ctx)
			if requestID == "" {
				request, ok := khttp.RequestFromServerContext(ctx)
				if !ok {
					return nil, kerrors.InternalServer("GATEWAY_CONTEXT", "http request is missing")
				}
				requestID = request.Header.Get("X-Request-ID")
				if requestID == "" {
					requestID = newRequestID()
				}
				ctx = context.WithValue(ctx, requestIDKey, requestID)
			}
			if response, ok := khttp.ResponseWriterFromServerContext(ctx); ok {
				response.Header().Set("X-Request-ID", requestID)
			}
			return next(ctx, req)
		}
	}
}

func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if requestID, ok := ctx.Value(requestIDKey).(string); ok {
		return requestID
	}
	return ""
}

func ClaimsFromContext(ctx context.Context) (pkgauth.AccessTokenClaims, bool) {
	if ctx == nil {
		return pkgauth.AccessTokenClaims{}, false
	}
	claims, ok := ctx.Value(claimsKey).(pkgauth.AccessTokenClaims)
	return claims, ok
}

func RequestContextFromContext(ctx context.Context) (*commonv1.RequestContext, bool) {
	if ctx == nil {
		return nil, false
	}
	requestContext, ok := ctx.Value(requestContextKey).(*commonv1.RequestContext)
	return requestContext, ok && requestContext != nil
}

func newRequestID() string {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "request-id-unavailable"
	}
	return hex.EncodeToString(data)
}

package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"github.com/google/wire"
)

type contextKey string

const requestIDKey contextKey = "request_id"

var ProviderSet = wire.NewSet(NewAuthMiddleware)

func RequestIDFilter() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := r.Header.Get("X-Request-ID")
			if requestID == "" {
				requestID = newRequestID()
			}
			w.Header().Set("X-Request-ID", requestID)
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, requestID)))
		})
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

func newRequestID() string {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "request-id-unavailable"
	}
	return hex.EncodeToString(data)
}

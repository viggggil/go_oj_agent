package middleware

import (
	"context"
	"testing"
)

func TestBearerToken(t *testing.T) {
	token, ok := BearerToken("Bearer access-token")
	if !ok || token != "access-token" {
		t.Fatalf("BearerToken() = %q/%v, want access-token/true", token, ok)
	}

	if token, ok := BearerToken("Basic access-token"); ok || token != "" {
		t.Fatalf("BearerToken() = %q/%v, want empty/false", token, ok)
	}
}

func TestAuthMiddlewareRequiresHTTPTransportContext(t *testing.T) {
	middleware, err := NewAuthMiddleware(testAuthConfig())
	if err != nil {
		t.Fatalf("NewAuthMiddleware() error = %v", err)
	}
	handler := middleware.Middleware()(func(context.Context, interface{}) (interface{}, error) {
		t.Fatal("下游处理器不应被调用")
		return nil, nil
	})
	_, err = handler(context.Background(), nil)
	if err == nil || err.Error() == "" {
		t.Fatalf("Middleware() error = %v, want unauthenticated error", err)
	}
}

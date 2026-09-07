package middleware

import "testing"

func TestBearerToken(t *testing.T) {
	token, ok := BearerToken("Bearer access-token")
	if !ok || token != "access-token" {
		t.Fatalf("BearerToken() = %q/%v, want access-token/true", token, ok)
	}

	if token, ok := BearerToken("Basic access-token"); ok || token != "" {
		t.Fatalf("BearerToken() = %q/%v, want empty/false", token, ok)
	}
}

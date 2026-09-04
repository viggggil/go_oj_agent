package biz

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestHMACTokenManagerIssueAndValidateAccessToken(t *testing.T) {
	now := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)
	manager, err := NewHMACTokenManager(TokenConfig{
		Secret:          "test-secret",
		Issuer:          "go-oj-agent",
		Audience:        "go-oj-gateway",
		AccessTokenTTL:  15 * time.Minute,
		RefreshTokenTTL: 7 * 24 * time.Hour,
		Now: func() time.Time {
			return now
		},
	})
	if err != nil {
		t.Fatalf("NewHMACTokenManager() error = %v", err)
	}

	token, expiresIn, err := manager.IssueAccessToken(context.Background(), User{
		ID:       1001,
		Username: "alice",
		Roles:    []RoleName{RoleUser, RoleAdmin},
	})
	if err != nil {
		t.Fatalf("IssueAccessToken() error = %v", err)
	}
	if expiresIn != 15*time.Minute {
		t.Fatalf("expiresIn = %s, want 15m", expiresIn)
	}
	if parts := strings.Split(token, "."); len(parts) != 3 {
		t.Fatalf("token parts = %d, want 3", len(parts))
	}

	claims, err := manager.ValidateAccessToken(token)
	if err != nil {
		t.Fatalf("ValidateAccessToken() error = %v", err)
	}
	if claims.Subject != 1001 || claims.Username != "alice" || !containsRole(claims.Roles, RoleAdmin) {
		t.Fatalf("claims = %#v, want alice admin", claims)
	}
	if claims.Issuer != "go-oj-agent" || claims.Audience != "go-oj-gateway" {
		t.Fatalf("claims issuer/audience = %q/%q", claims.Issuer, claims.Audience)
	}
}

func TestHMACTokenManagerRejectsTamperedAccessToken(t *testing.T) {
	manager, err := NewHMACTokenManager(TokenConfig{
		Secret:          "test-secret",
		AccessTokenTTL:  time.Minute,
		RefreshTokenTTL: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewHMACTokenManager() error = %v", err)
	}

	token, _, err := manager.IssueAccessToken(context.Background(), User{ID: 1001})
	if err != nil {
		t.Fatalf("IssueAccessToken() error = %v", err)
	}
	tampered := token[:len(token)-1] + "x"
	if _, err := manager.ValidateAccessToken(tampered); err != ErrInvalidCredential {
		t.Fatalf("ValidateAccessToken() error = %v, want ErrInvalidCredential", err)
	}
}

func TestHMACTokenManagerGeneratesRefreshTokenRecord(t *testing.T) {
	now := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)
	manager, err := NewHMACTokenManager(TokenConfig{
		Secret:          "test-secret",
		AccessTokenTTL:  time.Minute,
		RefreshTokenTTL: 7 * 24 * time.Hour,
		Now: func() time.Time {
			return now
		},
	})
	if err != nil {
		t.Fatalf("NewHMACTokenManager() error = %v", err)
	}

	raw, record, err := manager.Generate(1001, "session-1", "old-token")
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if raw == "" || record.TokenHash == "" || record.TokenHash == raw {
		t.Fatalf("raw/hash = %q/%q, want non-empty hashed token", raw, record.TokenHash)
	}
	if record.UserID != 1001 || record.SessionID != "session-1" || record.RotatedFrom != "old-token" {
		t.Fatalf("record = %#v, want user/session/rotated-from", record)
	}
	if !record.ExpiresAt.Equal(now.Add(7 * 24 * time.Hour)) {
		t.Fatalf("ExpiresAt = %s, want %s", record.ExpiresAt, now.Add(7*24*time.Hour))
	}
}

func containsRole(roles []RoleName, want RoleName) bool {
	for _, role := range roles {
		if role == want {
			return true
		}
	}
	return false
}

package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestHMACVerifierVerify(t *testing.T) {
	now := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	verifier, err := NewHMACVerifier(HMACVerifierConfig{
		Secret:   "test-secret",
		Issuer:   "go-oj-agent",
		Audience: "go-oj-gateway",
		Now: func() time.Time {
			return now
		},
	})
	if err != nil {
		t.Fatalf("NewHMACVerifier() error = %v", err)
	}

	token := testJWT(t, "test-secret", map[string]string{"alg": "HS256", "typ": "JWT"}, AccessTokenClaims{
		Subject:   1001,
		Username:  "alice",
		Roles:     []string{"user"},
		Issuer:    "go-oj-agent",
		Audience:  "go-oj-gateway",
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(time.Minute).Unix(),
		TokenID:   "token-1",
	})

	claims, err := verifier.Verify(token)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if claims.Subject != 1001 || claims.Username != "alice" || claims.Roles[0] != "user" {
		t.Fatalf("claims = %#v, want alice user", claims)
	}
}

func TestHMACVerifierRejectsInvalidTokens(t *testing.T) {
	now := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		token   func(t *testing.T) string
		wantErr error
	}{
		{
			name: "签名被篡改",
			token: func(t *testing.T) string {
				token := testJWT(t, "test-secret", validHeader(), validClaims(now))
				return token[:len(token)-1] + "x"
			},
			wantErr: ErrInvalidToken,
		},
		{
			name: "算法不允许",
			token: func(t *testing.T) string {
				return testJWT(t, "test-secret", map[string]string{"alg": "none", "typ": "JWT"}, validClaims(now))
			},
			wantErr: ErrInvalidToken,
		},
		{
			name: "issuer 不匹配",
			token: func(t *testing.T) string {
				claims := validClaims(now)
				claims.Issuer = "other"
				return testJWT(t, "test-secret", validHeader(), claims)
			},
			wantErr: ErrInvalidToken,
		},
		{
			name: "audience 不匹配",
			token: func(t *testing.T) string {
				claims := validClaims(now)
				claims.Audience = "other"
				return testJWT(t, "test-secret", validHeader(), claims)
			},
			wantErr: ErrInvalidToken,
		},
		{
			name: "令牌已过期",
			token: func(t *testing.T) string {
				claims := validClaims(now)
				claims.ExpiresAt = now.Add(-time.Second).Unix()
				return testJWT(t, "test-secret", validHeader(), claims)
			},
			wantErr: ErrExpiredToken,
		},
		{
			name: "缺少 subject",
			token: func(t *testing.T) string {
				claims := validClaims(now)
				claims.Subject = 0
				return testJWT(t, "test-secret", validHeader(), claims)
			},
			wantErr: ErrInvalidToken,
		},
	}

	verifier, err := NewHMACVerifier(HMACVerifierConfig{
		Secret:   "test-secret",
		Issuer:   "go-oj-agent",
		Audience: "go-oj-gateway",
		Now: func() time.Time {
			return now
		},
	})
	if err != nil {
		t.Fatalf("NewHMACVerifier() error = %v", err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := verifier.Verify(tt.token(t)); !errors.Is(err, tt.wantErr) {
				t.Fatalf("Verify() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func validHeader() map[string]string {
	return map[string]string{"alg": "HS256", "typ": "JWT"}
}

func validClaims(now time.Time) AccessTokenClaims {
	return AccessTokenClaims{
		Subject:   1001,
		Username:  "alice",
		Roles:     []string{"user"},
		Issuer:    "go-oj-agent",
		Audience:  "go-oj-gateway",
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(time.Minute).Unix(),
		TokenID:   "token-1",
	}
}

func testJWT(t *testing.T, secret string, header map[string]string, claims AccessTokenClaims) string {
	t.Helper()
	headerJSON, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	payloadJSON, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	parts := []string{
		base64.RawURLEncoding.EncodeToString(headerJSON),
		base64.RawURLEncoding.EncodeToString(payloadJSON),
	}
	signed := strings.Join(parts, ".")
	return signed + "." + signHS256([]byte(signed), []byte(secret))
}

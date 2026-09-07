package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

type HMACVerifierConfig struct {
	Secret   string
	Issuer   string
	Audience string
	Now      func() time.Time
}

// HMACVerifier 校验 user-service 签发的 HS256 Access Token。
type HMACVerifier struct {
	secret   []byte
	issuer   string
	audience string
	now      func() time.Time
}

func NewHMACVerifier(config HMACVerifierConfig) (*HMACVerifier, error) {
	if strings.TrimSpace(config.Secret) == "" {
		return nil, ErrInvalidToken
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &HMACVerifier{
		secret:   []byte(config.Secret),
		issuer:   config.Issuer,
		audience: config.Audience,
		now:      now,
	}, nil
}

func (v *HMACVerifier) Verify(token string) (AccessTokenClaims, error) {
	if v == nil || len(v.secret) == 0 || strings.TrimSpace(token) == "" {
		return AccessTokenClaims{}, ErrInvalidToken
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return AccessTokenClaims{}, ErrInvalidToken
	}

	// 先按原始 header.payload 计算签名，再解析 claims，避免篡改 payload 后被继续使用。
	signed := parts[0] + "." + parts[1]
	want := signHS256([]byte(signed), v.secret)
	if !hmac.Equal([]byte(parts[2]), []byte(want)) {
		return AccessTokenClaims{}, ErrInvalidToken
	}

	header, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return AccessTokenClaims{}, ErrInvalidToken
	}
	var jwtHeader map[string]string
	if err := json.Unmarshal(header, &jwtHeader); err != nil {
		return AccessTokenClaims{}, ErrInvalidToken
	}
	if jwtHeader["alg"] != "HS256" || jwtHeader["typ"] != "JWT" {
		return AccessTokenClaims{}, ErrInvalidToken
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return AccessTokenClaims{}, ErrInvalidToken
	}
	var claims AccessTokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return AccessTokenClaims{}, ErrInvalidToken
	}
	if claims.Subject <= 0 {
		return AccessTokenClaims{}, ErrInvalidToken
	}
	now := v.now().UTC().Unix()
	if claims.ExpiresAt <= now {
		return AccessTokenClaims{}, ErrExpiredToken
	}
	if v.issuer != "" && claims.Issuer != v.issuer {
		return AccessTokenClaims{}, ErrInvalidToken
	}
	if v.audience != "" && claims.Audience != v.audience {
		return AccessTokenClaims{}, ErrInvalidToken
	}
	return claims, nil
}

func signHS256(payload []byte, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

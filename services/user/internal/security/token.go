package security

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	pkgauth "github.com/viggggil/go_oj_agent/pkg/auth"
)

type TokenPair struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    time.Duration
}

type AccessTokenClaims = pkgauth.AccessTokenClaims

type RefreshTokenRecord struct {
	UserID       int64
	SessionID    string
	TokenID      string
	TokenHash    string
	CreatedAt    time.Time
	ExpiresAt    time.Time
	LastUsedAt   time.Time
	RotatedFrom  string
	Revoked      bool
	ReplayLocked bool
}

type TokenConfig struct {
	Secret          string
	Issuer          string
	Audience        string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	Now             func() time.Time
}

type TokenSubject struct {
	ID       int64
	Username string
	Roles    []string
}

type HMACTokenManager struct {
	secret          []byte
	issuer          string
	audience        string
	accessTokenTTL  time.Duration
	refreshTokenTTL time.Duration
	now             func() time.Time
}

func NewHMACTokenManager(config TokenConfig) (*HMACTokenManager, error) {
	if strings.TrimSpace(config.Secret) == "" {
		return nil, ErrInvalidArgument
	}
	if config.AccessTokenTTL <= 0 || config.RefreshTokenTTL <= 0 {
		return nil, ErrInvalidArgument
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &HMACTokenManager{
		secret:          []byte(config.Secret),
		issuer:          config.Issuer,
		audience:        config.Audience,
		accessTokenTTL:  config.AccessTokenTTL,
		refreshTokenTTL: config.RefreshTokenTTL,
		now:             now,
	}, nil
}

func (m *HMACTokenManager) IssueAccessToken(_ context.Context, subject TokenSubject) (string, time.Duration, error) {
	if m == nil || len(m.secret) == 0 || subject.ID == 0 {
		return "", 0, ErrInvalidArgument
	}

	now := m.now().UTC()
	tokenID, err := randomHex(16)
	if err != nil {
		return "", 0, err
	}
	claims := AccessTokenClaims{
		Subject:   subject.ID,
		Username:  subject.Username,
		Roles:     subject.Roles,
		Issuer:    m.issuer,
		Audience:  m.audience,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(m.accessTokenTTL).Unix(),
		TokenID:   tokenID,
	}
	token, err := m.signJWT(claims)
	if err != nil {
		return "", 0, err
	}
	return token, m.accessTokenTTL, nil
}

func (m *HMACTokenManager) ValidateAccessToken(token string) (AccessTokenClaims, error) {
	if m == nil {
		return AccessTokenClaims{}, ErrInvalidCredential
	}
	verifier, err := pkgauth.NewHMACVerifier(pkgauth.HMACVerifierConfig{
		Secret:   string(m.secret),
		Issuer:   m.issuer,
		Audience: m.audience,
		Now:      m.now,
	})
	if err != nil {
		return AccessTokenClaims{}, ErrInvalidCredential
	}
	claims, err := verifier.Verify(token)
	if err != nil {
		if errors.Is(err, pkgauth.ErrInvalidToken) || errors.Is(err, pkgauth.ErrExpiredToken) {
			return AccessTokenClaims{}, ErrInvalidCredential
		}
		return AccessTokenClaims{}, err
	}
	return claims, nil
}

func (m *HMACTokenManager) Generate(
	userID int64,
	sessionID string,
	rotatedFrom string,
) (string, RefreshTokenRecord, error) {
	if m == nil || userID == 0 {
		return "", RefreshTokenRecord{}, ErrInvalidArgument
	}

	// Refresh Token 只向客户端返回随机原文，服务端持久化时仅保存 SHA-256 hash。
	raw, err := randomURLToken(32)
	if err != nil {
		return "", RefreshTokenRecord{}, err
	}
	if sessionID == "" {
		sessionID, err = randomHex(16)
		if err != nil {
			return "", RefreshTokenRecord{}, err
		}
	}
	tokenID, err := randomHex(16)
	if err != nil {
		return "", RefreshTokenRecord{}, err
	}
	now := m.now().UTC()
	return raw, RefreshTokenRecord{
		UserID:      userID,
		SessionID:   sessionID,
		TokenID:     tokenID,
		TokenHash:   m.Hash(raw),
		CreatedAt:   now,
		ExpiresAt:   now.Add(m.refreshTokenTTL),
		LastUsedAt:  now,
		RotatedFrom: rotatedFrom,
	}, nil
}

func (m *HMACTokenManager) Hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (m *HMACTokenManager) signJWT(claims AccessTokenClaims) (string, error) {
	header := map[string]string{
		"alg": "HS256",
		"typ": "JWT",
	}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	payloadJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	encodedHeader := base64.RawURLEncoding.EncodeToString(headerJSON)
	encodedPayload := base64.RawURLEncoding.EncodeToString(payloadJSON)
	signed := encodedHeader + "." + encodedPayload
	return signed + "." + signHS256([]byte(signed), m.secret), nil
}

func signHS256(payload []byte, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func randomURLToken(size int) (string, error) {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func randomHex(size int) (string, error) {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}

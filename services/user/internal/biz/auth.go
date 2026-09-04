package biz

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"
)

const BcryptMaxPasswordBytes = 72

type PasswordPolicy struct {
	MinLength int
	MaxBytes  int
}

func DefaultPasswordPolicy() PasswordPolicy {
	return PasswordPolicy{
		MinLength: 8,
		MaxBytes:  BcryptMaxPasswordBytes,
	}
}

func (p PasswordPolicy) Validate(password string) error {
	password = strings.TrimSpace(password)
	if password == "" {
		return ErrInvalidArgument
	}
	if utf8.RuneCountInString(password) < p.MinLength {
		return ErrInvalidArgument
	}
	if len(password) > p.MaxBytes {
		return ErrInvalidArgument
	}
	return nil
}

type TokenPair struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    time.Duration
}

type AccessTokenClaims struct {
	Subject   int64      `json:"sub"`
	Username  string     `json:"username"`
	Roles     []RoleName `json:"roles"`
	Issuer    string     `json:"iss"`
	Audience  string     `json:"aud"`
	IssuedAt  int64      `json:"iat"`
	ExpiresAt int64      `json:"exp"`
	TokenID   string     `json:"jti"`
}

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

func (m *HMACTokenManager) IssueAccessToken(_ context.Context, user User) (string, time.Duration, error) {
	if m == nil || len(m.secret) == 0 || user.ID == 0 {
		return "", 0, ErrInvalidArgument
	}

	now := m.now().UTC()
	tokenID, err := randomHex(16)
	if err != nil {
		return "", 0, err
	}
	claims := AccessTokenClaims{
		Subject:   user.ID,
		Username:  user.Username,
		Roles:     user.Roles,
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
	if m == nil || len(m.secret) == 0 {
		return AccessTokenClaims{}, ErrInvalidCredential
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return AccessTokenClaims{}, ErrInvalidCredential
	}

	// 先按原始 header.payload 计算签名，再解析 claims，避免篡改 payload 后被继续使用。
	signed := parts[0] + "." + parts[1]
	want := signHS256([]byte(signed), m.secret)
	if !hmac.Equal([]byte(parts[2]), []byte(want)) {
		return AccessTokenClaims{}, ErrInvalidCredential
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return AccessTokenClaims{}, ErrInvalidCredential
	}
	header, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return AccessTokenClaims{}, ErrInvalidCredential
	}
	var jwtHeader map[string]string
	if err := json.Unmarshal(header, &jwtHeader); err != nil {
		return AccessTokenClaims{}, ErrInvalidCredential
	}
	if jwtHeader["alg"] != "HS256" || jwtHeader["typ"] != "JWT" {
		return AccessTokenClaims{}, ErrInvalidCredential
	}

	var claims AccessTokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return AccessTokenClaims{}, ErrInvalidCredential
	}
	now := m.now().UTC().Unix()
	if claims.ExpiresAt <= now {
		return AccessTokenClaims{}, ErrInvalidCredential
	}
	if m.issuer != "" && claims.Issuer != m.issuer {
		return AccessTokenClaims{}, ErrInvalidCredential
	}
	if m.audience != "" && claims.Audience != m.audience {
		return AccessTokenClaims{}, ErrInvalidCredential
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

func IsSupportedRole(role RoleName) bool {
	switch role {
	case RoleUser, RoleAdmin:
		return true
	default:
		return false
	}
}

func IsSupportedUserStatus(status UserStatus) bool {
	switch status {
	case UserStatusActive, UserStatusDisabled, UserStatusLocked:
		return true
	default:
		return false
	}
}

func IsBlank(value string) bool {
	return strings.TrimSpace(value) == ""
}

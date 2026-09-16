package auth

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrInvalidKey = errors.New("invalid rsa key")

type RS256Signer struct {
	privateKey *rsa.PrivateKey
	keyID      string
	issuer     string
	audience   string
	ttl        time.Duration
	now        func() time.Time
}

type RS256SignerConfig struct {
	PrivateKeyPEM           []byte
	KeyID, Issuer, Audience string
	TTL                     time.Duration
	Now                     func() time.Time
}

func NewRS256Signer(c RS256SignerConfig) (*RS256Signer, error) {
	if strings.TrimSpace(c.KeyID) == "" || c.TTL <= 0 {
		return nil, ErrInvalidKey
	}
	key, err := parseRSAPrivateKey(c.PrivateKeyPEM)
	if err != nil {
		return nil, err
	}
	now := c.Now
	if now == nil {
		now = time.Now
	}
	return &RS256Signer{privateKey: key, keyID: c.KeyID, issuer: c.Issuer, audience: c.Audience, ttl: c.TTL, now: now}, nil
}

func (s *RS256Signer) Sign(claims AccessTokenClaims) (string, error) {
	if s == nil || s.privateKey == nil || claims.Subject <= 0 {
		return "", ErrInvalidToken
	}
	now := s.now().UTC()
	claims.Issuer = s.issuer
	claims.Audience = s.audience
	claims.IssuedAt = now.Unix()
	claims.ExpiresAt = now.Add(s.ttl).Unix()
	header := map[string]string{"alg": "RS256", "typ": "JWT", "kid": s.keyID}
	h, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	p, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	input := base64.RawURLEncoding.EncodeToString(h) + "." + base64.RawURLEncoding.EncodeToString(p)
	digest := sha256.Sum256([]byte(input))
	sig, err := rsa.SignPKCS1v15(rand.Reader, s.privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return input + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

type RS256Verifier struct {
	keys             map[string]*rsa.PublicKey
	issuer, audience string
	now              func() time.Time
	skew             time.Duration
}
type RS256VerifierConfig struct {
	PublicKeys       map[string][]byte
	Issuer, Audience string
	Now              func() time.Time
	ClockSkew        time.Duration
}

func NewRS256Verifier(c RS256VerifierConfig) (*RS256Verifier, error) {
	if len(c.PublicKeys) == 0 {
		return nil, ErrInvalidKey
	}
	keys := make(map[string]*rsa.PublicKey, len(c.PublicKeys))
	for id, pemBytes := range c.PublicKeys {
		k, err := parseRSAPublicKey(pemBytes)
		if err != nil {
			return nil, fmt.Errorf("key %s: %w", id, err)
		}
		keys[id] = k
	}
	now := c.Now
	if now == nil {
		now = time.Now
	}
	return &RS256Verifier{keys: keys, issuer: c.Issuer, audience: c.Audience, now: now, skew: c.ClockSkew}, nil
}

func (v *RS256Verifier) Verify(token string) (AccessTokenClaims, error) {
	if v == nil {
		return AccessTokenClaims{}, ErrInvalidToken
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return AccessTokenClaims{}, ErrInvalidToken
	}
	hb, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return AccessTokenClaims{}, ErrInvalidToken
	}
	var h struct{ Alg, Typ, Kid string }
	if json.Unmarshal(hb, &h) != nil || h.Alg != "RS256" || h.Typ != "JWT" || h.Kid == "" {
		return AccessTokenClaims{}, ErrInvalidToken
	}
	key := v.keys[h.Kid]
	if key == nil {
		return AccessTokenClaims{}, ErrInvalidToken
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return AccessTokenClaims{}, ErrInvalidToken
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], sig) != nil {
		return AccessTokenClaims{}, ErrInvalidToken
	}
	pb, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return AccessTokenClaims{}, ErrInvalidToken
	}
	var c AccessTokenClaims
	if json.Unmarshal(pb, &c) != nil || c.Subject <= 0 {
		return AccessTokenClaims{}, ErrInvalidToken
	}
	now := v.now().UTC()
	skew := v.skew
	if c.ExpiresAt <= now.Add(-skew).Unix() {
		return AccessTokenClaims{}, ErrExpiredToken
	}
	if c.IssuedAt > now.Add(skew).Unix() {
		return AccessTokenClaims{}, ErrInvalidToken
	}
	if v.issuer != "" && c.Issuer != v.issuer {
		return AccessTokenClaims{}, ErrInvalidToken
	}
	if v.audience != "" && c.Audience != v.audience {
		return AccessTokenClaims{}, ErrInvalidToken
	}
	return c, nil
}

func parseRSAPrivateKey(b []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, ErrInvalidKey
	}
	if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if rsaKey, ok := k.(*rsa.PrivateKey); ok {
			return rsaKey, nil
		}
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	return nil, ErrInvalidKey
}
func parseRSAPublicKey(b []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, ErrInvalidKey
	}
	if k, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		if rsaKey, ok := k.(*rsa.PublicKey); ok {
			return rsaKey, nil
		}
	}
	if k, err := x509.ParsePKCS1PublicKey(block.Bytes); err == nil {
		return k, nil
	}
	return nil, ErrInvalidKey
}

package internalauth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"strings"
	"time"
)

var ErrInvalidToken = errors.New("invalid internal token")

type Claims struct {
	Issuer     string   `json:"iss"`
	Audience   string   `json:"aud"`
	Subject    string   `json:"sub"`
	ActorID    int64    `json:"actor_id,omitempty"`
	ActorRoles []string `json:"actor_roles,omitempty"`
	RPC        string   `json:"rpc"`
	RequestID  string   `json:"request_id,omitempty"`
	TraceID    string   `json:"trace_id,omitempty"`
	IssuedAt   int64    `json:"iat"`
	ExpiresAt  int64    `json:"exp"`
	TokenID    string   `json:"jti"`
}

type Principal struct {
	Caller                      string
	ActorID                     int64
	ActorRoles                  []string
	RequestID, TraceID, TokenID string
}

// MatchesRequestContext is a migration guard while protobuf RequestContext
// fields are being removed. It rejects a body identity that differs from the
// signed Gateway assertion.
func (p Principal) MatchesRequestContext(userID int64, roles []string) bool {
	if p.ActorID != userID || len(p.ActorRoles) != len(roles) {
		return false
	}
	set := make(map[string]struct{}, len(p.ActorRoles))
	for _, role := range p.ActorRoles {
		set[role] = struct{}{}
	}
	for _, role := range roles {
		if _, ok := set[role]; !ok {
			return false
		}
	}
	return true
}

type principalKey struct{}

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

type Signer struct {
	key                            *rsa.PrivateKey
	kid, issuer, audience, subject string
	ttl                            time.Duration
	now                            func() time.Time
}

func NewSigner(privatePEM []byte, kid, issuer, audience, subject string, ttl time.Duration, now func() time.Time) (*Signer, error) {
	block, _ := pem.Decode(privatePEM)
	if block == nil || kid == "" || issuer == "" || audience == "" || subject == "" || ttl <= 0 || ttl > time.Minute {
		return nil, ErrInvalidToken
	}
	v, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, ErrInvalidToken
	}
	key, ok := v.(*rsa.PrivateKey)
	if !ok {
		return nil, ErrInvalidToken
	}
	if now == nil {
		now = time.Now
	}
	return &Signer{key: key, kid: kid, issuer: issuer, audience: audience, subject: subject, ttl: ttl, now: now}, nil
}
func (s *Signer) Sign(c Claims) (string, error) {
	if s == nil || s.key == nil || c.RPC == "" || c.TokenID == "" {
		return "", ErrInvalidToken
	}
	now := s.now().UTC()
	c.Issuer = s.issuer
	c.Audience = s.audience
	c.Subject = s.subject
	c.IssuedAt = now.Unix()
	c.ExpiresAt = now.Add(s.ttl).Unix()
	h, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": s.kid})
	p, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	input := base64.RawURLEncoding.EncodeToString(h) + "." + base64.RawURLEncoding.EncodeToString(p)
	sum := sha256.Sum256([]byte(input))
	sig, err := rsa.SignPKCS1v15(rand.Reader, s.key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return input + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

type Verifier struct {
	keys                      map[string]*rsa.PublicKey
	issuer, audience, subject string
	maxTTL, skew              time.Duration
	now                       func() time.Time
}

func NewVerifier(publicKeys map[string][]byte, issuer, audience, subject string, maxTTL, skew time.Duration, now func() time.Time) (*Verifier, error) {
	if len(publicKeys) == 0 || maxTTL <= 0 || maxTTL > time.Minute {
		return nil, ErrInvalidToken
	}
	keys := map[string]*rsa.PublicKey{}
	for kid, b := range publicKeys {
		block, _ := pem.Decode(b)
		if block == nil {
			return nil, ErrInvalidToken
		}
		v, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, ErrInvalidToken
		}
		key, ok := v.(*rsa.PublicKey)
		if !ok {
			return nil, ErrInvalidToken
		}
		keys[kid] = key
	}
	if now == nil {
		now = time.Now
	}
	return &Verifier{keys: keys, issuer: issuer, audience: audience, subject: subject, maxTTL: maxTTL, skew: skew, now: now}, nil
}
func (v *Verifier) Verify(token, method string) (Claims, error) {
	var zero Claims
	parts := strings.Split(token, ".")
	if v == nil || len(parts) != 3 {
		return zero, ErrInvalidToken
	}
	hb, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return zero, ErrInvalidToken
	}
	var h struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
		Kid string `json:"kid"`
	}
	if json.Unmarshal(hb, &h) != nil || h.Alg != "RS256" || h.Typ != "JWT" || v.keys[h.Kid] == nil {
		return zero, ErrInvalidToken
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return zero, ErrInvalidToken
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if rsa.VerifyPKCS1v15(v.keys[h.Kid], crypto.SHA256, sum[:], sig) != nil {
		return zero, ErrInvalidToken
	}
	pb, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || json.Unmarshal(pb, &zero) != nil {
		return Claims{}, ErrInvalidToken
	}
	now := v.now().UTC().Unix()
	if zero.Issuer != v.issuer || zero.Audience != v.audience || zero.Subject != v.subject || zero.RPC != method || zero.TokenID == "" || zero.IssuedAt > now+int64(v.skew.Seconds()) || zero.ExpiresAt <= now-int64(v.skew.Seconds()) || zero.ExpiresAt-zero.IssuedAt > int64(v.maxTTL.Seconds()) {
		return Claims{}, ErrInvalidToken
	}
	return zero, nil
}

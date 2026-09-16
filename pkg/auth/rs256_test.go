package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"testing"
	"time"
)

func rsaPEM(t *testing.T) ([]byte, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	priv, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: priv}), pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pub})
}

func TestRS256SignVerifyAndKid(t *testing.T) {
	now := time.Date(2026, 9, 16, 10, 0, 0, 0, time.UTC)
	priv, pub := rsaPEM(t)
	signer, err := NewRS256Signer(RS256SignerConfig{PrivateKeyPEM: priv, KeyID: "user-1", Issuer: "user-service", Audience: "gateway", TTL: time.Minute, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	token, err := signer.Sign(AccessTokenClaims{Subject: 7, Username: "alice", Roles: []string{"admin"}})
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewRS256Verifier(RS256VerifierConfig{PublicKeys: map[string][]byte{"user-1": pub}, Issuer: "user-service", Audience: "gateway", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := verifier.Verify(token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != 7 || claims.Issuer != "user-service" {
		t.Fatalf("claims=%+v", claims)
	}
}

func TestRS256RejectsTamperAlgorithmAndAudience(t *testing.T) {
	now := time.Date(2026, 9, 16, 10, 0, 0, 0, time.UTC)
	priv, pub := rsaPEM(t)
	signer, _ := NewRS256Signer(RS256SignerConfig{PrivateKeyPEM: priv, KeyID: "k", Issuer: "issuer", Audience: "aud", TTL: time.Minute, Now: func() time.Time { return now }})
	token, _ := signer.Sign(AccessTokenClaims{Subject: 1})
	verifier, _ := NewRS256Verifier(RS256VerifierConfig{PublicKeys: map[string][]byte{"k": pub}, Issuer: "issuer", Audience: "aud", Now: func() time.Time { return now }})
	if _, err := verifier.Verify(token[:len(token)-1] + "x"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("tamper err=%v", err)
	}
	bad := token[:len(token)-len("RS256")] + "HS256" // header mutation invalidates signature and algorithm
	if _, err := verifier.Verify(bad); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("algorithm err=%v", err)
	}
	other, _ := NewRS256Verifier(RS256VerifierConfig{PublicKeys: map[string][]byte{"k": pub}, Issuer: "issuer", Audience: "other", Now: func() time.Time { return now }})
	if _, err := other.Verify(token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("audience err=%v", err)
	}
}

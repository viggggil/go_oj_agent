package internalauth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"strings"
	"testing"
	"time"
)

func keys(t *testing.T) ([]byte, []byte) {
	t.Helper()
	k, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	priv, e := x509.MarshalPKCS8PrivateKey(k)
	if e != nil {
		t.Fatal(e)
	}
	pub, e := x509.MarshalPKIXPublicKey(&k.PublicKey)
	if e != nil {
		t.Fatal(e)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: priv}), pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pub})
}

func TestInternalTokenBindsCallerActorAndRPC(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	priv, pub := keys(t)
	s, e := NewSigner(priv, "gw-1", "gateway", "problem-service", "gateway-service", 30*time.Second, func() time.Time { return now })
	if e != nil {
		t.Fatal(e)
	}
	token, e := s.Sign(Claims{ActorID: 7, ActorRoles: []string{"admin"}, RPC: "/problem.v1.ProblemService/CreateProblem", TokenID: "jti"})
	if e != nil {
		t.Fatal(e)
	}
	v, e := NewVerifier(map[string][]byte{"gw-1": pub}, "gateway", "problem-service", "gateway-service", time.Minute, 5*time.Second, func() time.Time { return now })
	if e != nil {
		t.Fatal(e)
	}
	c, e := v.Verify(token, "/problem.v1.ProblemService/CreateProblem")
	if e != nil {
		t.Fatal(e)
	}
	if c.ActorID != 7 || c.ActorRoles[0] != "admin" {
		t.Fatalf("claims=%+v", c)
	}
	if _, e = v.Verify(token, "/problem.v1.ProblemService/ArchiveProblem"); !errors.Is(e, ErrInvalidToken) {
		t.Fatalf("cross-rpc err=%v", e)
	}
}

func TestInternalTokenRejectsWrongAudienceAndTamper(t *testing.T) {
	now := time.Now().UTC()
	priv, pub := keys(t)
	s, _ := NewSigner(priv, "gw-1", "gateway", "problem-service", "gateway-service", 30*time.Second, func() time.Time { return now })
	token, _ := s.Sign(Claims{RPC: "/x", TokenID: "jti"})
	wrong, _ := NewVerifier(map[string][]byte{"gw-1": pub}, "gateway", "other", "gateway-service", time.Minute, 0, func() time.Time { return now })
	if _, e := wrong.Verify(token, "/x"); !errors.Is(e, ErrInvalidToken) {
		t.Fatalf("aud err=%v", e)
	}
	valid, _ := NewVerifier(map[string][]byte{"gw-1": pub}, "gateway", "problem-service", "gateway-service", time.Minute, 0, func() time.Time { return now })
	parts := strings.Split(token, ".")
	signature := []byte(parts[2])
	middle := len(signature) / 2
	if signature[middle] == 'A' {
		signature[middle] = 'B'
	} else {
		signature[middle] = 'A'
	}
	tampered := strings.Join([]string{parts[0], parts[1], string(signature)}, ".")
	if _, e := valid.Verify(tampered, "/x"); !errors.Is(e, ErrInvalidToken) {
		t.Fatalf("tamper err=%v", e)
	}
}

func TestPrincipalMatchesRequestContext(t *testing.T) {
	p := Principal{ActorID: 7, ActorRoles: []string{"admin", "user"}}
	if !p.MatchesRequestContext(7, []string{"user", "admin"}) {
		t.Fatal("equivalent roles should match")
	}
	if p.MatchesRequestContext(8, []string{"admin", "user"}) {
		t.Fatal("different user must not match")
	}
	if p.MatchesRequestContext(7, []string{"admin"}) {
		t.Fatal("different roles must not match")
	}
}

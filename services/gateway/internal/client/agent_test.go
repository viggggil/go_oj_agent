package client

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"strings"
	"testing"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/conf"
	gatewaymw "github.com/viggggil/go_oj_agent/services/gateway/internal/middleware"
)

func agentConfig(t *testing.T, bits int) *conf.Bootstrap {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	file := t.TempDir() + "/private.pem"
	if err := os.WriteFile(file, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}), 0600); err != nil {
		t.Fatal(err)
	}
	return &conf.Bootstrap{
		Clients: &conf.ClientsProto{Agent: &conf.AgentProto{Enabled: true, Endpoint: "http://agent-service:8000"}},
		Auth:    &conf.AuthProto{InternalPrivateKeyFile: file, InternalIssuer: "go-oj-gateway", InternalKeyId: "test", InternalTokenTtl: "30s"},
	}
}

func TestAgentClientRejectsInvalidConfiguration(t *testing.T) {
	config := agentConfig(t, 2048)
	for name, mutate := range map[string]func(*conf.Bootstrap){
		"zero timeout":      func(c *conf.Bootstrap) { c.Clients.Agent.ConnectTimeout = "0s" },
		"large timeout":     func(c *conf.Bootstrap) { c.Clients.Agent.MaxDuration = "361s" },
		"invalid timeout":   func(c *conf.Bootstrap) { c.Clients.Agent.IdleTimeout = "bad" },
		"small body":        func(c *conf.Bootstrap) { c.Clients.Agent.MaxRequestBytes = 1 },
		"large frame":       func(c *conf.Bootstrap) { c.Clients.Agent.MaxFrameBytes = 1048577 },
		"large concurrency": func(c *conf.Bootstrap) { c.Clients.Agent.MaxConcurrent = 129 },
		"URL credentials":   func(c *conf.Bootstrap) { c.Clients.Agent.Endpoint = "http://user:password@agent" },
		"URL query":         func(c *conf.Bootstrap) { c.Clients.Agent.Endpoint = "http://agent?secret=1" },
		"URL path":          func(c *conf.Bootstrap) { c.Clients.Agent.Endpoint = "http://agent/chat" },
		"URL scheme":        func(c *conf.Bootstrap) { c.Clients.Agent.Endpoint = "file:///secret" },
		"wrong issuer":      func(c *conf.Bootstrap) { c.Auth.InternalIssuer = "other" },
		"short TTL":         func(c *conf.Bootstrap) { c.Auth.InternalTokenTtl = "500ms" },
		"long TTL":          func(c *conf.Bootstrap) { c.Auth.InternalTokenTtl = "61s" },
		"long kid":          func(c *conf.Bootstrap) { c.Auth.InternalKeyId = strings.Repeat("k", 129) },
	} {
		t.Run(name, func(t *testing.T) {
			copy := &conf.Bootstrap{Clients: &conf.ClientsProto{Agent: &conf.AgentProto{Enabled: true, Endpoint: config.Clients.Agent.Endpoint}}, Auth: &conf.AuthProto{InternalPrivateKeyFile: config.Auth.InternalPrivateKeyFile, InternalIssuer: config.Auth.InternalIssuer, InternalKeyId: config.Auth.InternalKeyId, InternalTokenTtl: config.Auth.InternalTokenTtl}}
			mutate(copy)
			if _, _, err := NewAgentClient(copy); err == nil || strings.Contains(err.Error(), "password") || strings.Contains(err.Error(), config.Auth.InternalPrivateKeyFile) {
				t.Fatalf("expected safe configuration failure, got %v", err)
			}
		})
	}
	if _, _, err := NewAgentClient(agentConfig(t, 1024)); err == nil {
		t.Fatal("weak RSA key accepted")
	}
}

func TestAgentClientDefaultsAndBoundedCapacity(t *testing.T) {
	disabled, closeDisabled, err := NewAgentClient(nil)
	if err != nil || disabled.Enabled() || disabled.Acquire() {
		t.Fatalf("disabled client=%v error=%v", disabled, err)
	}
	closeDisabled()
	config := agentConfig(t, 2048)
	config.Clients.Agent.MaxConcurrent = 1
	agent, cleanup, err := NewAgentClient(config)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if !agent.Acquire() || agent.Acquire() {
		t.Fatal("capacity is not bounded")
	}
	agent.Release()
	if !agent.Acquire() {
		t.Fatal("capacity was not released")
	}
	agent.Release()
}

func TestAgentDelegationContextMatchesPythonBounds(t *testing.T) {
	if ValidateAgentContext(context.Background()) == nil {
		t.Fatal("missing trusted context accepted")
	}
	for name, rc := range map[string]*commonv1.RequestContext{
		"invalid actor":      {UserId: 0, RequestId: "id"},
		"missing request ID": {UserId: 7},
		"long request ID":    {UserId: 7, RequestId: strings.Repeat("中", 129)},
		"many roles":         {UserId: 7, RequestId: "id", Roles: make([]string, 33)},
		"empty role":         {UserId: 7, RequestId: "id", Roles: []string{""}},
		"long role":          {UserId: 7, RequestId: "id", Roles: []string{strings.Repeat("中", 65)}},
		"invalid UTF8":       {UserId: 7, RequestId: "\xff"},
	} {
		t.Run(name, func(t *testing.T) {
			if ValidateAgentContext(gatewaymw.WithRequestContext(context.Background(), rc)) == nil {
				t.Fatal("invalid delegation context accepted")
			}
		})
	}
	rc := &commonv1.RequestContext{UserId: 7, RequestId: strings.Repeat("中", 128), Roles: []string{strings.Repeat("中", 64)}}
	if err := ValidateAgentContext(gatewaymw.WithRequestContext(context.Background(), rc)); err != nil {
		t.Fatalf("valid Unicode context rejected: %v", err)
	}
}

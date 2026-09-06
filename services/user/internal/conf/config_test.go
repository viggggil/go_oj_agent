package conf

import (
	"testing"
	"time"
)

func TestLoadConfigFromEnv(t *testing.T) {
	t.Setenv("USER_SERVICE_NAME", "custom-user")
	t.Setenv("USER_SERVICE_GRPC_ADDR", ":19001")
	t.Setenv("USER_MYSQL_DSN", "user:pass@tcp(127.0.0.1:3306)/oj_user?parseTime=true")
	t.Setenv("USER_REDIS_ADDR", "127.0.0.1:6380")
	t.Setenv("USER_REDIS_PASSWORD", "secret")
	t.Setenv("USER_REDIS_DB", "2")
	t.Setenv("USER_REDIS_NAMESPACE", "test:user")
	t.Setenv("USER_ACCESS_TOKEN_KEY", "jwt-secret")
	t.Setenv("USER_ACCESS_TOKEN_TTL", "20m")
	t.Setenv("USER_REFRESH_TOKEN_TTL", "168h")
	t.Setenv("USER_CONSUL_ENABLED", "false")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.Service.Name != "custom-user" {
		t.Fatalf("service name = %q, want custom-user", cfg.Service.Name)
	}
	if cfg.Server.GRPC.Address != ":19001" {
		t.Fatalf("grpc address = %q, want :19001", cfg.Server.GRPC.Address)
	}
	if cfg.Data.RedisDB != 2 || cfg.Data.RedisNamespace != "test:user" {
		t.Fatalf("redis config = %#v", cfg.Data)
	}
	if cfg.Auth.AccessTokenTTL != 20*time.Minute || cfg.Auth.RefreshTokenTTL != 168*time.Hour {
		t.Fatalf("auth ttl = %s/%s", cfg.Auth.AccessTokenTTL, cfg.Auth.RefreshTokenTTL)
	}
	if cfg.Registry.Consul.Enabled {
		t.Fatal("consul enabled = true, want false")
	}
}

func TestLoadConfigRequiresMySQLDSN(t *testing.T) {
	t.Setenv("USER_ACCESS_TOKEN_KEY", "jwt-secret")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("LoadConfig() error = nil, want non-nil")
	}
}

func TestLoadConfigRequiresAccessTokenKey(t *testing.T) {
	t.Setenv("USER_MYSQL_DSN", "user:pass@tcp(127.0.0.1:3306)/oj_user?parseTime=true")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("LoadConfig() error = nil, want non-nil")
	}
}

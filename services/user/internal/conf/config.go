package conf

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/wire"
)

var ProviderSet = wire.NewSet(LoadConfig, NewRegistry)

type Config struct {
	Service  ServiceConfig
	Server   ServerConfig
	Data     DataConfig
	Auth     AuthConfig
	Registry Registry
}

type ServiceConfig struct {
	Name string
}

type ServerConfig struct {
	GRPC GRPCConfig
}

type GRPCConfig struct {
	Address string
}

type DataConfig struct {
	MySQLDSN       string
	RedisAddr      string
	RedisPassword  string
	RedisDB        int
	RedisNamespace string
}

type AuthConfig struct {
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	AccessTokenKey  string
	Issuer          string
	Audience        string
	Password        PasswordConfig
}

type PasswordConfig struct {
	BcryptCost int
	MinLength  int
	MaxBytes   int
}

type Registry struct {
	Consul ConsulConfig
}

type ConsulConfig struct {
	Enabled    bool
	Address    string
	Scheme     string
	Datacenter string
	Token      string
	ServiceID  string
}

func DefaultConfig() Config {
	return Config{
		Service: ServiceConfig{
			Name: "user-service",
		},
		Server: ServerConfig{
			GRPC: GRPCConfig{
				Address: ":9001",
			},
		},
		Data: DataConfig{
			RedisAddr:      "127.0.0.1:6379",
			RedisNamespace: "go_oj_agent:user",
		},
		Auth: AuthConfig{
			AccessTokenTTL:  15 * time.Minute,
			RefreshTokenTTL: 7 * 24 * time.Hour,
			AccessTokenKey:  "",
			Issuer:          "go-oj-agent",
			Audience:        "go-oj-gateway",
			Password: PasswordConfig{
				BcryptCost: 12,
				MinLength:  8,
				MaxBytes:   72,
			},
		},
		Registry: Registry{
			Consul: ConsulConfig{
				Enabled: true,
				Address: "127.0.0.1:8500",
				Scheme:  "http",
			},
		},
	}
}

func LoadConfig() (*Config, error) {
	cfg := DefaultConfig()

	cfg.Service.Name = envString("USER_SERVICE_NAME", cfg.Service.Name)
	cfg.Server.GRPC.Address = envString("USER_SERVICE_GRPC_ADDR", cfg.Server.GRPC.Address)

	cfg.Data.MySQLDSN = strings.TrimSpace(os.Getenv("USER_MYSQL_DSN"))
	cfg.Data.RedisAddr = envString("USER_REDIS_ADDR", cfg.Data.RedisAddr)
	cfg.Data.RedisPassword = os.Getenv("USER_REDIS_PASSWORD")
	cfg.Data.RedisNamespace = envString("USER_REDIS_NAMESPACE", cfg.Data.RedisNamespace)
	redisDB, err := envInt("USER_REDIS_DB", cfg.Data.RedisDB)
	if err != nil {
		return nil, err
	}
	cfg.Data.RedisDB = redisDB

	cfg.Auth.AccessTokenKey = strings.TrimSpace(os.Getenv("USER_ACCESS_TOKEN_KEY"))
	cfg.Auth.Issuer = envString("USER_ACCESS_TOKEN_ISSUER", cfg.Auth.Issuer)
	cfg.Auth.Audience = envString("USER_ACCESS_TOKEN_AUDIENCE", cfg.Auth.Audience)
	if cfg.Auth.AccessTokenTTL, err = envDuration("USER_ACCESS_TOKEN_TTL", cfg.Auth.AccessTokenTTL); err != nil {
		return nil, err
	}
	if cfg.Auth.RefreshTokenTTL, err = envDuration("USER_REFRESH_TOKEN_TTL", cfg.Auth.RefreshTokenTTL); err != nil {
		return nil, err
	}

	if cfg.Registry.Consul.Enabled, err = envBool("USER_CONSUL_ENABLED", cfg.Registry.Consul.Enabled); err != nil {
		return nil, err
	}
	cfg.Registry.Consul.Address = envString("USER_CONSUL_ADDR", cfg.Registry.Consul.Address)
	cfg.Registry.Consul.Scheme = envString("USER_CONSUL_SCHEME", cfg.Registry.Consul.Scheme)
	cfg.Registry.Consul.Datacenter = strings.TrimSpace(os.Getenv("USER_CONSUL_DATACENTER"))
	cfg.Registry.Consul.Token = strings.TrimSpace(os.Getenv("USER_CONSUL_TOKEN"))
	cfg.Registry.Consul.ServiceID = strings.TrimSpace(os.Getenv("USER_CONSUL_SERVICE_ID"))

	if cfg.Data.MySQLDSN == "" {
		return nil, fmt.Errorf("USER_MYSQL_DSN is required")
	}
	if cfg.Auth.AccessTokenKey == "" {
		return nil, fmt.Errorf("USER_ACCESS_TOKEN_KEY is required")
	}
	return &cfg, nil
}

func NewRegistry(config *Config) *Registry {
	if config == nil {
		return nil
	}
	return &config.Registry
}

func envString(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func envInt(key string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s is invalid: %w", key, err)
	}
	return value, nil
}

func envBool(key string, fallback bool) (bool, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s is invalid: %w", key, err)
	}
	return value, nil
}

func envDuration(key string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s is invalid: %w", key, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s must be positive", key)
	}
	return value, nil
}

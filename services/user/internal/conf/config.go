package conf

import "time"

type Config struct {
	Service ServiceConfig
	Auth    AuthConfig
}

type ServiceConfig struct {
	Name string
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

func DefaultConfig() Config {
	return Config{
		Service: ServiceConfig{
			Name: "user-service",
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
	}
}

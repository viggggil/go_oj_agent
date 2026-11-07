package servicediscovery

import (
	"fmt"
	"strings"

	consul "github.com/go-kratos/kratos/contrib/registry/consul/v3"
	"github.com/go-kratos/kratos/v3/registry"
	"github.com/hashicorp/consul/api"
)

// Config contains the small Consul client configuration shared by callers.
// Keeping this independent from a service's generated protobuf config avoids
// coupling the discovery helper to any one service.
type Config struct {
	Enabled    bool
	Address    string
	Scheme     string
	Datacenter string
	Token      string
}

// New returns a Consul discovery client when enabled. A nil discovery is
// returned for the fixed-endpoint mode used by local development and tests.
func New(config Config) (registry.Discovery, error) {
	if !config.Enabled {
		return nil, nil
	}
	if strings.TrimSpace(config.Address) == "" {
		return nil, fmt.Errorf("consul address is required when discovery is enabled")
	}
	cfg := api.DefaultConfig()
	cfg.Address = config.Address
	if config.Scheme != "" {
		cfg.Scheme = config.Scheme
	}
	if config.Datacenter != "" {
		cfg.Datacenter = config.Datacenter
	}
	if config.Token != "" {
		cfg.Token = config.Token
	}
	client, err := api.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	return consul.New(client), nil
}

// Target returns a Kratos discovery target or the configured endpoint.
func Target(discoveryEnabled bool, serviceName, fallback string) string {
	if discoveryEnabled && strings.TrimSpace(serviceName) != "" {
		return "discovery:///" + strings.TrimPrefix(strings.TrimSpace(serviceName), "/")
	}
	return fallback
}

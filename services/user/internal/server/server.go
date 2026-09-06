package server

import (
	consul "github.com/go-kratos/kratos/contrib/registry/consul/v3"
	"github.com/google/wire"
	"github.com/hashicorp/consul/api"

	"github.com/viggggil/go_oj_agent/services/user/internal/conf"
)

var ProviderSet = wire.NewSet(
	conf.ProviderSet,
	NewRegistrar,
	NewGRPCServer,
	NewMiddlewares,
)

// NewRegistrar 创建 Consul 服务注册器，注册和注销由 kratos.App 统一管理。
func NewRegistrar(config *conf.Registry) *consul.Registry {
	if config == nil || !config.Consul.Enabled {
		return nil
	}

	cfg := api.DefaultConfig()
	if config.Consul.Address != "" {
		cfg.Address = config.Consul.Address
	}
	if config.Consul.Scheme != "" {
		cfg.Scheme = config.Consul.Scheme
	}
	if config.Consul.Datacenter != "" {
		cfg.Datacenter = config.Consul.Datacenter
	}
	if config.Consul.Token != "" {
		cfg.Token = config.Consul.Token
	}

	client, err := api.NewClient(cfg)
	if err != nil {
		panic(err)
	}
	return consul.New(client, consul.WithHealthCheck(true))
}

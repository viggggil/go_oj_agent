package server

import (
	consul "github.com/go-kratos/kratos/contrib/registry/consul/v3"
	"github.com/go-kratos/kratos/v3/registry"
	"github.com/google/wire"
	"github.com/hashicorp/consul/api"

	"github.com/viggggil/go_oj_agent/services/user/internal/conf"
)

var ProviderSet = wire.NewSet(
	NewRegistrar,
	NewGRPCServer,
	NewMiddlewares,
)

// NewRegistrar 创建 Consul 服务注册器，注册和注销由 kratos.App 统一管理。
func NewRegistrar(config *conf.Bootstrap) registry.Registrar {
	if config == nil || config.GetRegistry() == nil || config.GetRegistry().GetConsul() == nil || !config.GetRegistry().GetConsul().GetEnabled() {
		return nil
	}

	cfg := api.DefaultConfig()
	consulCfg := config.GetRegistry().GetConsul()
	if consulCfg.GetAddress() != "" {
		cfg.Address = consulCfg.GetAddress()
	}
	if consulCfg.GetScheme() != "" {
		cfg.Scheme = consulCfg.GetScheme()
	}
	if consulCfg.GetDatacenter() != "" {
		cfg.Datacenter = consulCfg.GetDatacenter()
	}
	if consulCfg.GetToken() != "" {
		cfg.Token = consulCfg.GetToken()
	}

	client, err := api.NewClient(cfg)
	if err != nil {
		panic(err)
	}
	return consul.New(client, consul.WithHealthCheck(true))
}

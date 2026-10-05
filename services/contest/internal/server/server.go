package server

import (
	consul "github.com/go-kratos/kratos/contrib/registry/consul/v3"
	"github.com/go-kratos/kratos/v3/registry"
	"github.com/google/wire"
	"github.com/hashicorp/consul/api"
	"github.com/viggggil/go_oj_agent/services/contest/internal/conf"
)

var ProviderSet = wire.NewSet(NewRegistrar, NewGRPCServer, NewMiddlewares, NewResultConsumerServer, NewCacheRelayServer, NewCacheMaintenanceServer)

func NewRegistrar(config *conf.Bootstrap) registry.Registrar {
	if config == nil || config.GetRegistry() == nil || config.GetRegistry().GetConsul() == nil || !config.GetRegistry().GetConsul().GetEnabled() {
		return nil
	}
	consulConfig := config.GetRegistry().GetConsul()
	cfg := api.DefaultConfig()
	if consulConfig.GetAddress() != "" {
		cfg.Address = consulConfig.GetAddress()
	}
	if consulConfig.GetScheme() != "" {
		cfg.Scheme = consulConfig.GetScheme()
	}
	if consulConfig.GetDatacenter() != "" {
		cfg.Datacenter = consulConfig.GetDatacenter()
	}
	if consulConfig.GetToken() != "" {
		cfg.Token = consulConfig.GetToken()
	}
	client, err := api.NewClient(cfg)
	if err != nil {
		panic(err)
	}
	return consul.New(client, consul.WithHealthCheck(true))
}

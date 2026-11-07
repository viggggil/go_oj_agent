package data

import (
	"github.com/go-kratos/kratos/v3/registry"
	grpcdiscovery "github.com/go-kratos/kratos/v3/transport/grpc/resolver/discovery"
	"github.com/viggggil/go_oj_agent/pkg/servicediscovery"
	"github.com/viggggil/go_oj_agent/services/contest/internal/conf"
	"google.golang.org/grpc"
)

func newServiceDiscovery(config *conf.Bootstrap) (registry.Discovery, error) {
	if config == nil || config.GetRegistry() == nil || config.GetRegistry().GetConsul() == nil {
		return nil, nil
	}
	c := config.GetRegistry().GetConsul()
	return servicediscovery.New(servicediscovery.Config{
		Enabled: c.GetEnabled(), Address: c.GetAddress(), Scheme: c.GetScheme(),
		Datacenter: c.GetDatacenter(), Token: c.GetToken(),
	})
}

func addDiscovery(options []grpc.DialOption, discovery registry.Discovery) []grpc.DialOption {
	if discovery == nil {
		return options
	}
	return append(options, grpc.WithResolvers(grpcdiscovery.NewBuilder(discovery)))
}

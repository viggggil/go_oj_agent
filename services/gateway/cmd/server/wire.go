//go:build wireinject

package main

import (
	"github.com/google/wire"

	"github.com/viggggil/go_oj_agent/services/gateway/internal/client"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/conf"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/middleware"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/server"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/service"
)

func initApp(bc *conf.Bootstrap) (*App, func(), error) {
	wire.Build(
		client.ProviderSet,
		middleware.ProviderSet,
		server.ProviderSet,
		service.ProviderSet,
		newApp,
	)
	return nil, nil, nil
}

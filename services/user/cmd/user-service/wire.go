//go:build wireinject

package main

import (
	"github.com/google/wire"

	"github.com/viggggil/go_oj_agent/services/user/internal/biz"
	"github.com/viggggil/go_oj_agent/services/user/internal/conf"
	"github.com/viggggil/go_oj_agent/services/user/internal/data"
	"github.com/viggggil/go_oj_agent/services/user/internal/server"
	userservice "github.com/viggggil/go_oj_agent/services/user/internal/service"
)

func initApp(bc *conf.Bootstrap) (*App, func(), error) {
	wire.Build(
		server.ProviderSet,
		data.ProviderSet,
		biz.ProviderSet,
		userservice.ProviderSet,
		newApp,
	)
	return nil, nil, nil
}

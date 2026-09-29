//go:build wireinject

package main

import (
	"github.com/google/wire"
	"github.com/viggggil/go_oj_agent/services/contest/internal/biz"
	"github.com/viggggil/go_oj_agent/services/contest/internal/conf"
	"github.com/viggggil/go_oj_agent/services/contest/internal/server"
	contestservice "github.com/viggggil/go_oj_agent/services/contest/internal/service"
)

func initApp(config *conf.Bootstrap) (*App, func(), error) {
	wire.Build(server.ProviderSet, biz.ProviderSet, contestservice.ProviderSet, newApp)
	return nil, nil, nil
}

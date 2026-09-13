//go:build wireinject

package main

import (
	"github.com/google/wire"

	"github.com/viggggil/go_oj_agent/services/problem/internal/biz"
	"github.com/viggggil/go_oj_agent/services/problem/internal/conf"
	"github.com/viggggil/go_oj_agent/services/problem/internal/server"
	problemservice "github.com/viggggil/go_oj_agent/services/problem/internal/service"
)

func initApp(bc *conf.Bootstrap) (*App, func(), error) {
	wire.Build(server.ProviderSet, biz.ProviderSet, problemservice.ProviderSet, newApp)
	return nil, nil, nil
}

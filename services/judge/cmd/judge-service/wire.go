//go:build wireinject

package main

import (
	"github.com/google/wire"

	"github.com/viggggil/go_oj_agent/services/judge/internal/biz"
	"github.com/viggggil/go_oj_agent/services/judge/internal/conf"
	"github.com/viggggil/go_oj_agent/services/judge/internal/data"
	"github.com/viggggil/go_oj_agent/services/judge/internal/server"
	judgeservice "github.com/viggggil/go_oj_agent/services/judge/internal/service"
)

func initApp(bc *conf.Bootstrap) (*App, func(), error) {
	wire.Build(server.ProviderSet, data.ProviderSet, biz.ProviderSet, judgeservice.ProviderSet, newApp)
	return nil, nil, nil
}

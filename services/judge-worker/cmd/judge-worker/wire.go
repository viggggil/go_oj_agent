//go:build wireinject

package main

import (
	"github.com/google/wire"

	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/conf"
	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/sandbox"
	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/server"
)

func initApp(config *conf.Bootstrap) (*App, func(), error) {
	wire.Build(sandbox.ProviderSet, server.NewWorker, newApp)
	return nil, nil, nil
}

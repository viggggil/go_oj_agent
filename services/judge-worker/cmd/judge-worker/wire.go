//go:build wireinject

package main

import (
	"github.com/google/wire"

	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/biz"
	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/comparator"
	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/conf"
	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/language"
	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/message"
	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/sandbox"
	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/server"
	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/storage"
)

func initApp(config *conf.Bootstrap) (*App, func(), error) {
	wire.Build(sandbox.ProviderSet, storage.ProviderSet, language.ProviderSet, comparator.ProviderSet, biz.NewEngine, message.NewReporterFromConfig, message.NewConsumerFromConfig, server.NewWorkerWithConsumer, newApp,
		wire.Bind(new(message.Engine), new(*biz.Engine)),
		wire.Bind(new(message.Reporter), new(*message.RabbitReporter)),
	)
	return nil, nil, nil
}

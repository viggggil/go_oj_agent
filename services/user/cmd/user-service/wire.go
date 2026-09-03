//go:build wireinject

package main

import (
	"github.com/google/wire"

	"github.com/viggggil/go_oj_agent/services/user/internal/biz"
	"github.com/viggggil/go_oj_agent/services/user/internal/conf"
	"github.com/viggggil/go_oj_agent/services/user/internal/server"
	userservice "github.com/viggggil/go_oj_agent/services/user/internal/service"
)

func initApp() (*App, func(), error) {
	wire.Build(
		conf.DefaultConfig,
		newUserUsecaseOptions,
		biz.NewUserUsecase,
		userservice.NewUserService,
		server.New,
		NewApp,
	)
	return nil, nil, nil
}

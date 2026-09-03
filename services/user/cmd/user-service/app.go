package main

import (
	"github.com/viggggil/go_oj_agent/services/user/internal/biz"
	"github.com/viggggil/go_oj_agent/services/user/internal/conf"
	"github.com/viggggil/go_oj_agent/services/user/internal/server"
)

type App struct {
	server *server.Server
}

func NewApp(server *server.Server) *App {
	return &App{server: server}
}

func newUserUsecaseOptions(cfg conf.Config) biz.UserUsecaseOptions {
	return biz.UserUsecaseOptions{
		Passwords: biz.NewBcryptPasswordHasher(cfg.Auth.Password.BcryptCost),
		PasswordPolicy: biz.PasswordPolicy{
			MinLength: cfg.Auth.Password.MinLength,
			MaxBytes:  cfg.Auth.Password.MaxBytes,
		},
	}
}

func (a *App) Run() error {
	if a == nil || a.server == nil {
		return nil
	}
	return a.server.Start()
}

func (a *App) Close() error {
	if a == nil || a.server == nil {
		return nil
	}
	return a.server.Stop()
}

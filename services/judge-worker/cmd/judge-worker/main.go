package main

import (
	"flag"
	"log/slog"
	"os"

	kratos "github.com/go-kratos/kratos/v3"
	"github.com/go-kratos/kratos/v3/config"
	"github.com/go-kratos/kratos/v3/config/env"
	"github.com/go-kratos/kratos/v3/config/file"

	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/conf"
	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/server"
)

type App = kratos.App

func main() {
	configPath := flag.String("conf", "services/judge-worker/configs/config.yaml", "config path")
	flag.Parse()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	source := config.New(config.WithSource(file.NewSource(*configPath), env.NewSource("KRATOS")))
	if err := source.Load(); err != nil {
		logger.Error("failed to load judge-worker config", "error", err)
		os.Exit(1)
	}
	var bootstrap conf.Bootstrap
	if err := source.Scan(&bootstrap); err != nil {
		logger.Error("failed to scan judge-worker config", "error", err)
		os.Exit(1)
	}
	if err := bootstrap.Validate(); err != nil {
		logger.Error("invalid judge-worker config", "error", err)
		os.Exit(1)
	}
	app, cleanup, err := initApp(&bootstrap)
	if err != nil {
		logger.Error("failed to initialize judge-worker", "error", err)
		os.Exit(1)
	}
	defer cleanup()
	if err = app.Run(); err != nil {
		logger.Error("judge-worker exited with error", "error", err)
		os.Exit(1)
	}
}

func newApp(config *conf.Bootstrap, worker *server.Worker) *kratos.App {
	return kratos.New(
		kratos.Name(config.Service.Name),
		kratos.Version("dev"),
		kratos.Server(worker),
	)
}

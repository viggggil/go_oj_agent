package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"time"

	kratos "github.com/go-kratos/kratos/v3"
	"github.com/go-kratos/kratos/v3/config"
	"github.com/go-kratos/kratos/v3/config/env"
	"github.com/go-kratos/kratos/v3/config/file"

	"github.com/viggggil/go_oj_agent/pkg/observability"
	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/conf"
	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/server"
)

type App = kratos.App

func main() {
	configPath := flag.String("conf", "services/judge-worker/configs/config.yaml", "config path")
	flag.Parse()
	logger := observability.NewJSONLogger(os.Stdout, "judge-worker")
	slog.SetDefault(logger)
	source := config.New(config.WithSource(file.NewSource(*configPath), env.NewSource("KRATOS")))
	if err := source.Load(); err != nil {
		logger.Error("failed to load judge-worker config", "error_code", "JUDGE_CONFIG_LOAD_FAILED", "error", err)
		os.Exit(1)
	}
	var bootstrap conf.Bootstrap
	if err := source.Scan(&bootstrap); err != nil {
		logger.Error("failed to scan judge-worker config", "error_code", "JUDGE_CONFIG_SCAN_FAILED", "error", err)
		os.Exit(1)
	}
	if err := bootstrap.Validate(); err != nil {
		logger.Error("invalid judge-worker config", "error_code", "JUDGE_CONFIG_INVALID", "error", err)
		os.Exit(1)
	}
	shutdownTelemetry, telemetryErr := observability.Init(context.Background(), "judge-worker")
	if telemetryErr != nil {
		logger.Error("failed to initialize OpenTelemetry", "error_code", "OTEL_INIT_FAILED", "error_type", "exporter_init")
		shutdownTelemetry = func(context.Context) error { return nil }
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := shutdownTelemetry(ctx); err != nil {
			logger.Error("failed to shut down OpenTelemetry", "error_code", "OTEL_SHUTDOWN_FAILED", "error_type", "shutdown")
		}
	}()
	app, cleanup, err := initApp(&bootstrap)
	if err != nil {
		logger.Error("failed to initialize judge-worker", "error_code", "JUDGE_INIT_FAILED", "error", err)
		os.Exit(1)
	}
	defer cleanup()
	if err = app.Run(); err != nil {
		logger.Error("judge-worker exited with error", "error_code", "JUDGE_EXITED", "error", err)
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

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
	"github.com/go-kratos/kratos/v3/registry"
	kgrpc "github.com/go-kratos/kratos/v3/transport/grpc"
	"github.com/viggggil/go_oj_agent/pkg/observability"
	"github.com/viggggil/go_oj_agent/services/contest/internal/conf"
	"github.com/viggggil/go_oj_agent/services/contest/internal/server"
)

type App = kratos.App

func main() {
	flagconf := flag.String("conf", "services/contest/configs/config.yaml", "config path")
	flag.Parse()
	logger := newJSONLogger(os.Stdout)
	errorLogger := newJSONLogger(os.Stderr)
	slog.SetDefault(logger)
	c := config.New(config.WithSource(file.NewSource(*flagconf), env.NewSource("KRATOS")))
	if err := c.Load(); err != nil {
		errorLogger.Error("failed to load config", "error_code", "CONTEST_CONFIG_LOAD_FAILED", "error", err)
		os.Exit(1)
	}
	var bootstrap conf.Bootstrap
	if err := c.Scan(&bootstrap); err != nil {
		errorLogger.Error("failed to scan config", "error_code", "CONTEST_CONFIG_SCAN_FAILED", "error", err)
		os.Exit(1)
	}
	if err := conf.ValidateConfig(&bootstrap); err != nil {
		errorLogger.Error("invalid contest service config", "error_code", "CONTEST_CONFIG_INVALID", "error", err)
		os.Exit(1)
	}
	shutdownTelemetry, telemetryErr := observability.Init(context.Background(), "contest-service")
	if telemetryErr != nil {
		errorLogger.Error("failed to initialize OpenTelemetry", "error_code", "OTEL_INIT_FAILED", "error_type", "exporter_init")
		shutdownTelemetry = func(context.Context) error { return nil }
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := shutdownTelemetry(ctx); err != nil {
			errorLogger.Error("failed to shut down OpenTelemetry", "error_code", "OTEL_SHUTDOWN_FAILED", "error_type", "shutdown")
		}
	}()
	app, cleanup, err := initApp(&bootstrap)
	if err != nil {
		errorLogger.Error("failed to initialize contest-service", "error_code", "CONTEST_INIT_FAILED", "error", err)
		os.Exit(1)
	}
	defer cleanup()
	logger.Info("starting contest-service")
	if err := app.Run(); err != nil {
		errorLogger.Error("contest-service exited with error", "error_code", "CONTEST_EXITED", "error", err)
		os.Exit(1)
	}
}

func newJSONLogger(output *os.File) *slog.Logger {
	return observability.NewJSONLogger(output, "contest-service")
}

func newApp(bootstrap *conf.Bootstrap, grpcServer *kgrpc.Server, registrar registry.Registrar, consumer *server.ResultConsumerServer, cacheRelay *server.CacheRelayServer, maintenance *server.CacheMaintenanceServer) (*kratos.App, error) {
	if err := consumer.Prepare(context.Background()); err != nil {
		return nil, err
	}
	options := []kratos.Option{
		kratos.Name(bootstrap.GetService().GetName()),
		kratos.Version("dev"),
		kratos.Server(grpcServer, consumer, cacheRelay, maintenance, server.NewMetricsServer(bootstrap)),
		kratos.Logger(newJSONLogger(os.Stdout)),
	}
	if registrar != nil {
		options = append(options, kratos.Registrar(registrar))
	}
	return kratos.New(options...), nil
}

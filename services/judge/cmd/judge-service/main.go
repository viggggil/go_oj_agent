package main

import (
	"flag"
	"log/slog"
	"os"

	kratos "github.com/go-kratos/kratos/v3"
	"github.com/go-kratos/kratos/v3/config"
	"github.com/go-kratos/kratos/v3/config/env"
	"github.com/go-kratos/kratos/v3/config/file"
	"github.com/go-kratos/kratos/v3/registry"
	kgrpc "github.com/go-kratos/kratos/v3/transport/grpc"

	"github.com/viggggil/go_oj_agent/services/judge/internal/conf"
	"github.com/viggggil/go_oj_agent/services/judge/internal/server"
)

type App = kratos.App

func main() {
	flagconf := flag.String("conf", "services/judge/configs/config.yaml", "config path")
	flag.Parse()

	logger := newJSONLogger(os.Stdout)
	errorLogger := newJSONLogger(os.Stderr)
	c := config.New(config.WithSource(file.NewSource(*flagconf), env.NewSource("KRATOS")))
	if err := c.Load(); err != nil {
		errorLogger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	var bc conf.Bootstrap
	if err := c.Scan(&bc); err != nil {
		errorLogger.Error("failed to scan config", "error", err)
		os.Exit(1)
	}

	app, cleanup, err := initApp(&bc)
	if err != nil {
		errorLogger.Error("failed to initialize judge-service", "error", err)
		os.Exit(1)
	}
	defer cleanup()

	logger.Info("starting judge-service")
	if err := app.Run(); err != nil {
		errorLogger.Error("judge-service exited with error", "error", err)
		os.Exit(1)
	}
}

func newJSONLogger(output *os.File) *slog.Logger {
	return slog.New(slog.NewJSONHandler(output, &slog.HandlerOptions{}))
}

func newApp(config *conf.Bootstrap, grpcServer *kgrpc.Server, relayServer *server.RelayServer, resultConsumerServer *server.ResultConsumerServer, registrar registry.Registrar) *kratos.App {
	options := []kratos.Option{
		kratos.Name(config.GetService().GetName()),
		kratos.Version("dev"),
		kratos.Server(grpcServer),
		kratos.Server(relayServer),
		kratos.Server(resultConsumerServer),
		kratos.Logger(newJSONLogger(os.Stdout)),
	}
	if registrar != nil {
		options = append(options, kratos.Registrar(registrar))
	}
	return kratos.New(options...)
}

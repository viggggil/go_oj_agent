package main

import (
	"log/slog"
	"os"

	kratos "github.com/go-kratos/kratos/v3"
	kgrpc "github.com/go-kratos/kratos/v3/transport/grpc"

	consul "github.com/go-kratos/kratos/contrib/registry/consul/v3"

	"github.com/viggggil/go_oj_agent/services/user/internal/conf"
)

type App = kratos.App

func main() {
	logger := newJSONLogger(os.Stdout)
	errorLogger := newJSONLogger(os.Stderr)

	app, cleanup, err := initApp()
	if err != nil {
		errorLogger.Error("failed to initialize user-service", "error", err)
		os.Exit(1)
	}
	defer func() {
		cleanup()
	}()

	logger.Info("starting user-service")
	if err := app.Run(); err != nil {
		errorLogger.Error("user-service exited with error", "error", err)
		os.Exit(1)
	}
}

func newJSONLogger(output *os.File) *slog.Logger {
	return slog.New(slog.NewJSONHandler(output, &slog.HandlerOptions{}))
}

// newApp 组装 Kratos 应用，由 Kratos 统一管理 Server 和 Registrar 生命周期。
func newApp(
	config *conf.Config,
	grpcServer *kgrpc.Server,
	registrar *consul.Registry,
) *kratos.App {
	options := []kratos.Option{
		kratos.Name(config.Service.Name),
		kratos.Version("dev"),
		kratos.Server(grpcServer),
		kratos.Logger(newJSONLogger(os.Stdout)),
	}
	if registrar != nil {
		options = append(options, kratos.Registrar(registrar))
	}
	return kratos.New(options...)
}

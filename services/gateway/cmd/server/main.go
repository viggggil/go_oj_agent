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
	khttp "github.com/go-kratos/kratos/v3/transport/http"

	"github.com/viggggil/go_oj_agent/services/gateway/internal/conf"
)

type App = kratos.App

func main() {
	flagconf := flag.String("conf", "services/gateway/configs/config.yaml", "config path")
	flag.Parse()

	logger := newJSONLogger(os.Stdout)
	errorLogger := newJSONLogger(os.Stderr)

	c := config.New(
		config.WithSource(
			file.NewSource(*flagconf),
			env.NewSource("KRATOS"),
		),
	)
	if err := c.Load(); err != nil {
		errorLogger.Error("加载配置失败", "error", err)
		os.Exit(1)
	}

	var bc conf.Bootstrap
	if err := c.Scan(&bc); err != nil {
		errorLogger.Error("解析配置失败", "error", err)
		os.Exit(1)
	}

	app, cleanup, err := initApp(&bc)
	if err != nil {
		errorLogger.Error("初始化 gateway-service 失败", "error", err)
		os.Exit(1)
	}
	defer cleanup()

	logger.Info("启动 gateway-service")
	if err := app.Run(); err != nil {
		errorLogger.Error("gateway-service 异常退出", "error", err)
		os.Exit(1)
	}
}

func newJSONLogger(output *os.File) *slog.Logger {
	return slog.New(slog.NewJSONHandler(output, &slog.HandlerOptions{}))
}

// newApp 组装 Kratos 应用，由 Kratos 统一管理 Server 和 Registrar 生命周期。
func newApp(
	config *conf.Bootstrap,
	httpServer *khttp.Server,
	registrar registry.Registrar,
) *kratos.App {
	options := []kratos.Option{
		kratos.Name(config.GetService().GetName()),
		kratos.Version("dev"),
		kratos.Server(httpServer),
		kratos.Logger(newJSONLogger(os.Stdout)),
	}
	if registrar != nil {
		options = append(options, kratos.Registrar(registrar))
	}
	return kratos.New(options...)
}

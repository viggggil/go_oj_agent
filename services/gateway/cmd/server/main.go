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
	khttp "github.com/go-kratos/kratos/v3/transport/http"

	"github.com/viggggil/go_oj_agent/pkg/observability"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/conf"
)

type App = kratos.App

func main() {
	flagconf := flag.String("conf", "services/gateway/configs/config.yaml", "config path")
	flag.Parse()

	logger := newJSONLogger(os.Stdout)
	errorLogger := newJSONLogger(os.Stderr)
	slog.SetDefault(logger)

	c := config.New(
		config.WithSource(
			file.NewSource(*flagconf),
			env.NewSource("KRATOS"),
		),
	)
	if err := c.Load(); err != nil {
		errorLogger.Error("加载配置失败", "error_code", "GATEWAY_CONFIG_LOAD_FAILED", "error", err)
		os.Exit(1)
	}

	var bc conf.Bootstrap
	if err := c.Scan(&bc); err != nil {
		errorLogger.Error("解析配置失败", "error_code", "GATEWAY_CONFIG_SCAN_FAILED", "error", err)
		os.Exit(1)
	}
	if err := applyAgentEnableOverride(c, &bc); err != nil {
		errorLogger.Error("解析 Agent 配置失败", "error_code", "GATEWAY_AGENT_CONFIG_FAILED", "error", err)
		os.Exit(1)
	}
	shutdownTelemetry, telemetryErr := observability.Init(context.Background(), "gateway-service")
	if telemetryErr != nil {
		// Telemetry failure must not prevent the gateway from serving traffic.
		errorLogger.Error("初始化 OpenTelemetry 失败", "error_code", "OTEL_INIT_FAILED", "error_type", "exporter_init")
		shutdownTelemetry = func(context.Context) error { return nil }
	}

	app, cleanup, err := initApp(&bc)
	if err != nil {
		errorLogger.Error("初始化 gateway-service 失败", "error_code", "GATEWAY_INIT_FAILED", "error", err)
		os.Exit(1)
	}
	defer cleanup()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = shutdownTelemetry(ctx)
	}()

	logger.Info("启动 gateway-service")
	if err := app.Run(); err != nil {
		errorLogger.Error("gateway-service 异常退出", "error_code", "GATEWAY_EXITED", "error", err)
		os.Exit(1)
	}
}

func newJSONLogger(output *os.File) *slog.Logger {
	return observability.NewJSONLogger(output, "gateway-service")
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

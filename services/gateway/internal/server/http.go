package server

import (
	"time"

	"github.com/go-kratos/kratos/v3/middleware/recovery"
	"github.com/go-kratos/kratos/v3/middleware/validate"
	khttp "github.com/go-kratos/kratos/v3/transport/http"

	gatewayv1 "github.com/viggggil/go_oj_agent/api/gateway/v1"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/conf"
	gatewaymw "github.com/viggggil/go_oj_agent/services/gateway/internal/middleware"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/service"
)

func NewHTTPServer(
	config *conf.Bootstrap,
	authMiddleware *gatewaymw.AuthMiddleware,
	gatewayService *service.GatewayService,
) *khttp.Server {
	address := ":8080"
	timeout := 3 * time.Second
	if config != nil && config.GetServer() != nil && config.GetServer().GetHttp() != nil {
		if config.GetServer().GetHttp().GetAddress() != "" {
			address = config.GetServer().GetHttp().GetAddress()
		}
		if parsed, err := time.ParseDuration(config.GetServer().GetHttp().GetTimeout()); err == nil && parsed > 0 {
			timeout = parsed
		}
	}

	server := khttp.NewServer(
		khttp.Address(address),
		khttp.Timeout(timeout),
		khttp.Middleware(
			gatewaymw.RequestIDMiddleware(),
			recovery.Recovery(),
			validate.Validator(),
		),
	)
	gatewayv1.RegisterGatewayServiceHTTPServer(server, gatewayService)
	server.Use(
		gatewayv1.OperationGatewayServiceGetCurrentUser,
		authMiddleware.Middleware(),
	)
	server.Use(
		gatewayv1.OperationGatewayServiceGetUser,
		authMiddleware.Middleware(),
	)
	return server
}

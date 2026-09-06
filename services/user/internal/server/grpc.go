package server

import (
	"github.com/go-kratos/kratos/v3/middleware"
	kgrpc "github.com/go-kratos/kratos/v3/transport/grpc"

	userv1 "github.com/viggggil/go_oj_agent/api/user/v1"
	"github.com/viggggil/go_oj_agent/services/user/internal/conf"
	userservice "github.com/viggggil/go_oj_agent/services/user/internal/service"
)

// NewGRPCServer 构建 gRPC Server，并注册 user-service handler。
func NewGRPCServer(
	config *conf.Config,
	middlewares []middleware.Middleware,
	userService *userservice.UserService,
) *kgrpc.Server {
	address := ":9001"
	if config != nil && config.Server.GRPC.Address != "" {
		address = config.Server.GRPC.Address
	}
	options := []kgrpc.ServerOption{kgrpc.Address(address)}
	if len(middlewares) > 0 {
		options = append(options, kgrpc.Middleware(middlewares...))
	}

	server := kgrpc.NewServer(options...)
	userv1.RegisterUserServiceServer(server, userService)
	return server
}

func NewMiddlewares() []middleware.Middleware {
	return nil
}

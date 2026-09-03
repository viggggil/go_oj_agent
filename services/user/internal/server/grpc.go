package server

import (
	"github.com/go-kratos/kratos/v3/middleware"
	kgrpc "github.com/go-kratos/kratos/v3/transport/grpc"

	userv1 "github.com/viggggil/go_oj_agent/api/user/v1"
	userservice "github.com/viggggil/go_oj_agent/services/user/internal/service"
)

// NewGRPCServer 创建 user-service 的 gRPC 传输层。
// Proto 代码生成后，由 register 回调注册具体的 service handler。
func NewGRPCServer(
	address string,
	middlewares []middleware.Middleware,
	register func(*kgrpc.Server),
) *kgrpc.Server {
	options := []kgrpc.ServerOption{
		kgrpc.Address(address),
	}
	if len(middlewares) > 0 {
		options = append(options, kgrpc.Middleware(middlewares...))
	}

	server := kgrpc.NewServer(options...)
	if register != nil {
		register(server)
	}
	return server
}

// RegisterUserService 将 user-service handler 注册到 gRPC Server。
func RegisterUserService(server *kgrpc.Server, userService *userservice.UserService) {
	if server == nil || userService == nil {
		return
	}
	userv1.RegisterUserServiceServer(server, userService)
}

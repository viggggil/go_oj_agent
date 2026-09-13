package server

import (
	"github.com/go-kratos/kratos/v3/middleware"
	kgrpc "github.com/go-kratos/kratos/v3/transport/grpc"
	"google.golang.org/grpc"

	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"github.com/viggggil/go_oj_agent/services/problem/internal/conf"
	problemservice "github.com/viggggil/go_oj_agent/services/problem/internal/service"
)

func NewGRPCServer(
	config *conf.Bootstrap,
	middlewares []middleware.Middleware,
	problemService *problemservice.ProblemService,
) *kgrpc.Server {
	address := ":9002"
	if config != nil && config.GetServer() != nil && config.GetServer().GetGrpc() != nil && config.GetServer().GetGrpc().GetAddress() != "" {
		address = config.GetServer().GetGrpc().GetAddress()
	}
	options := []kgrpc.ServerOption{kgrpc.Address(address)}
	maxReceiveMessageBytes := 34 * 1024 * 1024
	if config != nil && config.GetServer() != nil && config.GetServer().GetGrpc() != nil && config.GetServer().GetGrpc().GetMaxReceiveMessageBytes() > 0 {
		maxReceiveMessageBytes = int(config.GetServer().GetGrpc().GetMaxReceiveMessageBytes())
	}
	options = append(options, kgrpc.Options(grpc.MaxRecvMsgSize(maxReceiveMessageBytes)))
	if len(middlewares) > 0 {
		options = append(options, kgrpc.Middleware(middlewares...))
	}

	server := kgrpc.NewServer(options...)
	problemv1.RegisterProblemServiceServer(server, problemService)
	return server
}

func NewMiddlewares() []middleware.Middleware {
	return nil
}

package server

import (
	"github.com/go-kratos/kratos/v3/middleware"
	kgrpc "github.com/go-kratos/kratos/v3/transport/grpc"
	"google.golang.org/grpc"
	"os"
	"time"

	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"github.com/viggggil/go_oj_agent/pkg/internalauth"
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
	if config != nil {
		if auth := config.GetInternalAuth(); auth != nil && auth.GetPublicKeyFile() != "" {
			key, err := os.ReadFile(auth.GetPublicKeyFile())
			if err != nil {
				panic(err)
			}
			maxTTL, err := time.ParseDuration(auth.GetMaxTokenTtl())
			if err != nil {
				panic(err)
			}
			skew, err := time.ParseDuration(auth.GetClockSkew())
			if err != nil {
				panic(err)
			}
			verifier, err := internalauth.NewVerifier(map[string][]byte{auth.GetKeyId(): key}, auth.GetIssuer(), auth.GetAudience(), auth.GetSubject(), maxTTL, skew, nil)
			if err != nil {
				panic(err)
			}
			options = append(options, kgrpc.Options(grpc.ChainUnaryInterceptor(internalauth.UnaryServerInterceptor(verifier))))
		}
	}
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

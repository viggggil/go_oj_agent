package server

import (
	"os"
	"time"

	"github.com/go-kratos/kratos/v3/middleware"
	kgrpc "github.com/go-kratos/kratos/v3/transport/grpc"
	"google.golang.org/grpc"

	submissionv1 "github.com/viggggil/go_oj_agent/api/submission/v1"
	"github.com/viggggil/go_oj_agent/pkg/internalauth"
	"github.com/viggggil/go_oj_agent/services/judge/internal/conf"
	judgeservice "github.com/viggggil/go_oj_agent/services/judge/internal/service"
)

func NewGRPCServer(
	config *conf.Bootstrap,
	middlewares []middleware.Middleware,
	submissionService *judgeservice.SubmissionService,
) (*kgrpc.Server, error) {
	address := ":9003"
	if config != nil && config.GetServer() != nil && config.GetServer().GetGrpc() != nil && config.GetServer().GetGrpc().GetAddress() != "" {
		address = config.GetServer().GetGrpc().GetAddress()
	}
	options := []kgrpc.ServerOption{kgrpc.Address(address)}
	if config != nil {
		if auth := config.GetInternalAuth(); auth != nil && auth.GetPublicKeyFile() != "" {
			key, err := os.ReadFile(auth.GetPublicKeyFile())
			if err != nil {
				return nil, err
			}
			maxTTL, err := time.ParseDuration(auth.GetMaxTokenTtl())
			if err != nil {
				return nil, err
			}
			skew, err := time.ParseDuration(auth.GetClockSkew())
			if err != nil {
				return nil, err
			}
			verifier, err := internalauth.NewVerifier(map[string][]byte{auth.GetKeyId(): key}, auth.GetIssuer(), auth.GetAudience(), auth.GetSubject(), maxTTL, skew, nil)
			if err != nil {
				return nil, err
			}
			options = append(options, kgrpc.Options(grpc.ChainUnaryInterceptor(internalauth.UnaryServerInterceptor(verifier))))
		}
	}
	if len(middlewares) > 0 {
		options = append(options, kgrpc.Middleware(middlewares...))
	}

	server := kgrpc.NewServer(options...)
	submissionv1.RegisterSubmissionServiceServer(server, submissionService)
	return server, nil
}

func NewMiddlewares() []middleware.Middleware {
	return nil
}

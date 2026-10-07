package server

import (
	"os"
	"time"

	"github.com/go-kratos/kratos/v3/middleware"
	kgrpc "github.com/go-kratos/kratos/v3/transport/grpc"
	contestv1 "github.com/viggggil/go_oj_agent/api/contest/v1"
	"github.com/viggggil/go_oj_agent/pkg/internalauth"
	"github.com/viggggil/go_oj_agent/services/contest/internal/conf"
	contestservice "github.com/viggggil/go_oj_agent/services/contest/internal/service"
	"google.golang.org/grpc"
)

func NewGRPCServer(config *conf.Bootstrap, middlewares []middleware.Middleware, contestService *contestservice.ContestService) (*kgrpc.Server, error) {
	address := ":9005"
	maxReceive := 34 * 1024 * 1024
	if config != nil && config.GetServer() != nil && config.GetServer().GetGrpc() != nil {
		grpcConfig := config.GetServer().GetGrpc()
		if grpcConfig.GetAddress() != "" {
			address = grpcConfig.GetAddress()
		}
		if grpcConfig.GetMaxReceiveMessageBytes() > 0 {
			maxReceive = int(grpcConfig.GetMaxReceiveMessageBytes())
		}
	}
	options := []kgrpc.ServerOption{kgrpc.Address(address), kgrpc.Options(grpc.MaxRecvMsgSize(maxReceive))}
	if config != nil {
		if auth := config.GetInternalAuth(); auth != nil && auth.GetPublicKeyFile() != "" {
			maxTTL, err := time.ParseDuration(auth.GetMaxTokenTtl())
			if err != nil {
				return nil, err
			}
			skew, err := time.ParseDuration(auth.GetClockSkew())
			if err != nil {
				return nil, err
			}
			callers := append([]*conf.InternalCallerProto{{PublicKeyFile: auth.GetPublicKeyFile(), KeyId: auth.GetKeyId(), Issuer: auth.GetIssuer(), Subject: auth.GetSubject()}}, auth.GetAdditionalCallers()...)
			verifiers := make([]internalauth.TokenVerifier, 0, len(callers))
			for _, caller := range callers {
				callerKey, readErr := os.ReadFile(caller.GetPublicKeyFile())
				if readErr != nil {
					return nil, readErr
				}
				verifier, verifyErr := internalauth.NewVerifier(map[string][]byte{caller.GetKeyId(): callerKey}, caller.GetIssuer(), auth.GetAudience(), caller.GetSubject(), maxTTL, skew, nil)
				if verifyErr != nil {
					return nil, verifyErr
				}
				restricted, verifyErr := internalauth.NewCallerMethodAllowlistVerifier(verifier, caller.GetRequireMethodAllowlist(), caller.GetAllowedRpcs())
				if verifyErr != nil {
					return nil, verifyErr
				}
				verifiers = append(verifiers, restricted)
			}
			verifierSet, err := internalauth.NewVerifierSet(verifiers...)
			if err != nil {
				return nil, err
			}
			options = append(options, kgrpc.Options(grpc.ChainUnaryInterceptor(internalauth.UnaryServerInterceptor(verifierSet))))
		}
	}
	if len(middlewares) > 0 {
		options = append(options, kgrpc.Middleware(middlewares...))
	}
	grpcServer := kgrpc.NewServer(options...)
	contestv1.RegisterContestServiceServer(grpcServer, contestService)
	return grpcServer, nil
}

func NewMiddlewares() []middleware.Middleware { return nil }

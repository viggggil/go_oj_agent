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
				key, readErr := os.ReadFile(caller.GetPublicKeyFile())
				if readErr != nil {
					return nil, readErr
				}
				verifier, verifyErr := internalauth.NewVerifier(map[string][]byte{caller.GetKeyId(): key}, caller.GetIssuer(), auth.GetAudience(), caller.GetSubject(), maxTTL, skew, nil)
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

	server := kgrpc.NewServer(options...)
	submissionv1.RegisterSubmissionServiceServer(server, submissionService)
	return server, nil
}

func NewMiddlewares() []middleware.Middleware {
	return nil
}

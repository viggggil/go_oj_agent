package client

import (
	"context"
	"fmt"
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"github.com/viggggil/go_oj_agent/pkg/internalauth"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/conf"
	gatewaymw "github.com/viggggil/go_oj_agent/services/gateway/internal/middleware"
	"google.golang.org/grpc"
	"os"
	"time"
)

type ProblemClient struct{ problemv1.ProblemServiceClient }

func NewProblemClient(ctx context.Context, config *conf.Bootstrap) (*ProblemClient, func(), error) {
	if config == nil || config.GetClients() == nil {
		return nil, nil, fmt.Errorf("gateway problem client config is required")
	}
	var interceptors []grpc.UnaryClientInterceptor
	auth := config.GetAuth()
	if auth != nil && auth.GetInternalPrivateKeyFile() != "" {
		key, readErr := os.ReadFile(auth.GetInternalPrivateKeyFile())
		if readErr != nil {
			return nil, nil, fmt.Errorf("read gateway internal private key: %w", readErr)
		}
		ttl, parseErr := time.ParseDuration(auth.GetInternalTokenTtl())
		if parseErr != nil {
			return nil, nil, fmt.Errorf("internal token ttl: %w", parseErr)
		}
		signer, signErr := internalauth.NewSigner(key, auth.GetInternalKeyId(), auth.GetInternalIssuer(), auth.GetInternalAudience(), "gateway-service", ttl, nil)
		if signErr != nil {
			return nil, nil, signErr
		}
		interceptors = append(interceptors, internalauth.UnaryClientInterceptor(signer, func(callCtx context.Context) internalauth.Actor {
			rc, _ := gatewaymw.RequestContextFromContext(callCtx)
			if rc == nil {
				return internalauth.Actor{}
			}
			return internalauth.Actor{ID: rc.GetUserId(), Roles: rc.GetRoles(), RequestID: rc.GetRequestId(), TraceID: rc.GetTraceId()}
		}))
	}
	conn, cleanup, err := newGRPCConn(ctx, config.GetClients().GetProblem(), interceptors...)
	if err != nil {
		return nil, nil, err
	}
	return &ProblemClient{problemv1.NewProblemServiceClient(conn)}, cleanup, nil
}
func ProvideProblemServiceClient(c *ProblemClient) problemv1.ProblemServiceClient {
	return c.ProblemServiceClient
}

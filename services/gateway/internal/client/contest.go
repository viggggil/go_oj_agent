package client

import (
	"context"
	"fmt"
	"os"
	"time"

	contestv1 "github.com/viggggil/go_oj_agent/api/contest/v1"
	"github.com/viggggil/go_oj_agent/pkg/internalauth"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/conf"
	gatewaymw "github.com/viggggil/go_oj_agent/services/gateway/internal/middleware"
	"google.golang.org/grpc"
)

type ContestClient struct{ contestv1.ContestServiceClient }

func NewContestClient(ctx context.Context, config *conf.Bootstrap) (*ContestClient, func(), error) {
	if config == nil || config.GetClients() == nil || config.GetClients().GetContest() == nil {
		return nil, nil, fmt.Errorf("gateway contest client config is required")
	}
	auth := config.GetAuth()
	var interceptors []grpc.UnaryClientInterceptor
	if auth != nil && auth.GetInternalPrivateKeyFile() != "" {
		key, err := os.ReadFile(auth.GetInternalPrivateKeyFile())
		if err != nil {
			return nil, nil, fmt.Errorf("read gateway internal private key: %w", err)
		}
		ttl, err := time.ParseDuration(auth.GetInternalTokenTtl())
		if err != nil {
			return nil, nil, err
		}
		signer, err := internalauth.NewSigner(key, auth.GetInternalKeyId(), auth.GetInternalIssuer(), internalAudience(config.GetClients().GetContest(), auth.GetInternalAudience()), "gateway-service", ttl, nil)
		if err != nil {
			return nil, nil, err
		}
		interceptors = append(interceptors, internalauth.UnaryClientInterceptor(signer, func(callCtx context.Context) internalauth.Actor {
			rc, _ := gatewaymw.RequestContextFromContext(callCtx)
			if rc == nil {
				return internalauth.Actor{}
			}
			return internalauth.Actor{ID: rc.GetUserId(), Roles: rc.GetRoles(), RequestID: rc.GetRequestId(), TraceID: rc.GetTraceId()}
		}))
	}
	conn, cleanup, err := newGRPCConn(ctx, config.GetClients().GetContest(), interceptors...)
	if err != nil {
		return nil, nil, err
	}
	return &ContestClient{contestv1.NewContestServiceClient(conn)}, cleanup, nil
}

func ProvideContestServiceClient(c *ContestClient) contestv1.ContestServiceClient {
	return c.ContestServiceClient
}

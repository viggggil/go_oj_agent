package client

import (
	"context"
	"fmt"
	"os"
	"time"

	submissionv1 "github.com/viggggil/go_oj_agent/api/submission/v1"
	"github.com/viggggil/go_oj_agent/pkg/internalauth"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/conf"
	gatewaymw "github.com/viggggil/go_oj_agent/services/gateway/internal/middleware"
	"google.golang.org/grpc"
)

type SubmissionClient struct {
	submissionv1.SubmissionServiceClient
}

func NewSubmissionClient(ctx context.Context, config *conf.Bootstrap) (*SubmissionClient, func(), error) {
	if config == nil || config.GetClients() == nil || config.GetClients().GetSubmission() == nil {
		return nil, nil, fmt.Errorf("gateway submission client config is required")
	}
	var interceptors []grpc.UnaryClientInterceptor
	auth := config.GetAuth()
	if auth != nil && auth.GetInternalPrivateKeyFile() != "" {
		key, err := os.ReadFile(auth.GetInternalPrivateKeyFile())
		if err != nil {
			return nil, nil, fmt.Errorf("read gateway internal private key: %w", err)
		}
		ttl, err := time.ParseDuration(auth.GetInternalTokenTtl())
		if err != nil {
			return nil, nil, fmt.Errorf("internal token ttl: %w", err)
		}
		signer, err := internalauth.NewSigner(key, auth.GetInternalKeyId(), auth.GetInternalIssuer(), auth.GetInternalAudience(), "gateway-service", ttl, nil)
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
	conn, cleanup, err := newGRPCConn(ctx, config.GetClients().GetSubmission(), interceptors...)
	if err != nil {
		return nil, nil, err
	}
	return &SubmissionClient{submissionv1.NewSubmissionServiceClient(conn)}, cleanup, nil
}

func ProvideSubmissionServiceClient(c *SubmissionClient) submissionv1.SubmissionServiceClient {
	return c.SubmissionServiceClient
}

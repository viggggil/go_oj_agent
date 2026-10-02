package data

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	submissionv1 "github.com/viggggil/go_oj_agent/api/submission/v1"
	"github.com/viggggil/go_oj_agent/pkg/internalauth"
	"github.com/viggggil/go_oj_agent/services/contest/internal/biz"
	"github.com/viggggil/go_oj_agent/services/contest/internal/conf"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var _ submissionv1.SubmissionServiceClient = (*submissionClient)(nil)

type submissionClient struct {
	submissionv1.SubmissionServiceClient
}

func NewClientContext() context.Context { return context.Background() }

func NewSubmissionClient(ctx context.Context, config *conf.Bootstrap) (*submissionClient, func(), error) {
	if config == nil || config.GetJudge() == nil || strings.TrimSpace(config.GetJudge().GetEndpoint()) == "" {
		return nil, func() {}, fmt.Errorf("contest judge client endpoint is required")
	}
	timeout := 3 * time.Second
	if parsed, err := time.ParseDuration(config.GetJudge().GetTimeout()); err == nil && parsed > 0 {
		timeout = parsed
	}
	cfg := config.GetJudge()
	var interceptors []grpc.UnaryClientInterceptor
	if strings.TrimSpace(cfg.GetPrivateKeyFile()) != "" {
		key, err := os.ReadFile(cfg.GetPrivateKeyFile())
		if err != nil {
			return nil, func() {}, fmt.Errorf("read contest judge private key: %w", err)
		}
		ttl, err := time.ParseDuration(cfg.GetTokenTtl())
		if err != nil || ttl <= 0 {
			return nil, func() {}, fmt.Errorf("contest judge token ttl must be positive")
		}
		signer, err := internalauth.NewSigner(key, cfg.GetKeyId(), cfg.GetIssuer(), cfg.GetAudience(), cfg.GetSubject(), ttl, nil)
		if err != nil {
			return nil, func() {}, err
		}
		interceptors = append(interceptors, internalauth.UnaryClientInterceptor(signer, func(callCtx context.Context) internalauth.Actor {
			principal, ok := internalauth.PrincipalFromContext(callCtx)
			if !ok {
				return internalauth.Actor{}
			}
			return internalauth.Actor{ID: principal.ActorID, Roles: principal.ActorRoles, RequestID: principal.RequestID, TraceID: principal.TraceID}
		}))
	}
	options := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock()}
	if len(interceptors) > 0 {
		options = append(options, grpc.WithChainUnaryInterceptor(interceptors...))
	}
	connectCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := grpc.DialContext(connectCtx, cfg.GetEndpoint(), options...)
	if err != nil {
		return nil, func() {}, err
	}
	return &submissionClient{submissionv1.NewSubmissionServiceClient(conn)}, func() { _ = conn.Close() }, nil
}

func ProvideSubmissionServiceClient(c *submissionClient) submissionv1.SubmissionServiceClient {
	return c.SubmissionServiceClient
}

func ProvideSubmissionCreator(c *submissionClient) biz.SubmissionCreator { return c }

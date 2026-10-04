package data

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"github.com/viggggil/go_oj_agent/pkg/internalauth"
	"github.com/viggggil/go_oj_agent/services/contest/internal/biz"
	"github.com/viggggil/go_oj_agent/services/contest/internal/conf"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var _ problemv1.ProblemServiceClient = (*problemClient)(nil)

type problemClient struct {
	problemv1.ProblemServiceClient
	timeout time.Duration
}

func NewProblemClient(ctx context.Context, config *conf.Bootstrap) (*problemClient, func(), error) {
	if config == nil || config.GetProblem() == nil || strings.TrimSpace(config.GetProblem().GetEndpoint()) == "" {
		return nil, func() {}, fmt.Errorf("contest problem client endpoint is required")
	}
	timeout := 3 * time.Second
	if parsed, err := time.ParseDuration(config.GetProblem().GetTimeout()); err == nil && parsed > 0 {
		timeout = parsed
	}
	cfg := config.GetProblem()
	var interceptors []grpc.UnaryClientInterceptor
	if strings.TrimSpace(cfg.GetPrivateKeyFile()) != "" {
		key, err := os.ReadFile(cfg.GetPrivateKeyFile())
		if err != nil {
			return nil, func() {}, fmt.Errorf("read contest problem private key: %w", err)
		}
		ttl, err := time.ParseDuration(cfg.GetTokenTtl())
		if err != nil || ttl <= 0 {
			return nil, func() {}, fmt.Errorf("contest problem token ttl must be positive")
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
	return &problemClient{ProblemServiceClient: problemv1.NewProblemServiceClient(conn), timeout: timeout}, func() { _ = conn.Close() }, nil
}

func ProvideProblemServiceClient(c *problemClient) problemv1.ProblemServiceClient {
	return c.ProblemServiceClient
}

func ProvideProblemCatalog(c *problemClient) biz.ProblemCatalog { return c }

func (c *problemClient) Titles(ctx context.Context, ids []int64) (map[int64]string, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	out, err := c.BatchGetProblems(ctx, &problemv1.BatchGetProblemsRequest{ProblemIds: ids})
	if err != nil {
		return nil, err
	}
	titles := make(map[int64]string, len(out.GetProblems()))
	for _, p := range out.GetProblems() {
		titles[p.GetId()] = p.GetTitle()
	}
	return titles, nil
}

package data

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	kgrpc "github.com/go-kratos/kratos/v3/transport/grpc"
	"github.com/oklog/ulid/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"github.com/viggggil/go_oj_agent/pkg/internalauth"
	"github.com/viggggil/go_oj_agent/services/judge/internal/biz"
	"github.com/viggggil/go_oj_agent/services/judge/internal/conf"
)

type ProblemClient struct {
	client problemv1.ProblemServiceClient
}

func NewProblemClient(config *conf.Bootstrap) (*ProblemClient, func(), error) {
	if config == nil || config.GetClients() == nil || config.GetClients().GetProblem() == nil {
		return nil, nil, fmt.Errorf("judge problem client config is required")
	}
	cfg := config.GetClients().GetProblem()
	if strings.TrimSpace(cfg.GetEndpoint()) == "" || strings.TrimSpace(cfg.GetPrivateKeyFile()) == "" {
		return nil, nil, fmt.Errorf("judge problem endpoint and private key are required")
	}
	privateKey, err := os.ReadFile(cfg.GetPrivateKeyFile())
	if err != nil {
		return nil, nil, fmt.Errorf("read judge internal private key: %w", err)
	}
	interceptor, err := newProblemAuthInterceptor(cfg, privateKey, nil)
	if err != nil {
		return nil, nil, err
	}
	timeout := 3 * time.Second
	if cfg.GetTimeout() != "" {
		timeout, err = time.ParseDuration(cfg.GetTimeout())
		if err != nil || timeout <= 0 {
			return nil, nil, fmt.Errorf("judge problem client timeout must be positive")
		}
	}
	conn, err := kgrpc.NewClient(context.Background(),
		kgrpc.WithEndpoint(cfg.GetEndpoint()),
		kgrpc.WithTimeout(timeout),
		kgrpc.WithUnaryInterceptor(interceptor),
	)
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { _ = conn.Close() }
	return &ProblemClient{client: problemv1.NewProblemServiceClient(conn)}, cleanup, nil
}

func newProblemAuthInterceptor(cfg *conf.ProblemClientProto, privateKey []byte, now func() time.Time) (grpc.UnaryClientInterceptor, error) {
	if cfg == nil || cfg.GetSubject() != "judge-service" {
		return nil, fmt.Errorf("judge problem client subject must be judge-service")
	}
	tokenTTL, err := time.ParseDuration(cfg.GetTokenTtl())
	if err != nil || tokenTTL <= 0 || tokenTTL > time.Minute {
		return nil, fmt.Errorf("judge problem token ttl must be between 0 and 1m")
	}
	signer, err := internalauth.NewSigner(privateKey, cfg.GetKeyId(), cfg.GetIssuer(), cfg.GetAudience(), cfg.GetSubject(), tokenTTL, now)
	if err != nil {
		return nil, fmt.Errorf("create judge problem signer: %w", err)
	}
	return internalauth.UnaryClientInterceptor(signer, resolveProblemActor), nil
}

func newProblemClient(client problemv1.ProblemServiceClient) *ProblemClient {
	return &ProblemClient{client: client}
}

func (c *ProblemClient) GetJudgeProfile(ctx context.Context, problemID int64) (biz.JudgeProfile, error) {
	if c == nil || c.client == nil {
		return biz.JudgeProfile{}, biz.ErrorInternal("problem catalog is not configured")
	}
	if problemID <= 0 {
		return biz.JudgeProfile{}, biz.ErrorInvalidArgument("invalid problem id")
	}
	response, err := c.client.GetJudgeProfile(ctx, &problemv1.GetJudgeProfileRequest{ProblemId: problemID})
	if err != nil {
		return biz.JudgeProfile{}, mapProblemError(err)
	}
	profile := response.GetProfile()
	if profile == nil || profile.GetProblemId() != problemID {
		return biz.JudgeProfile{}, biz.ErrorDependencyUnavailable("problem service returned an invalid judge profile")
	}
	if profile.GetStatus() != problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL {
		return biz.JudgeProfile{}, biz.ErrorProblemUnavailable("problem is not available for judging")
	}
	if profile.GetTimeLimitMs() <= 0 || profile.GetMemoryLimitKb() <= 0 {
		return biz.JudgeProfile{}, biz.ErrorDependencyUnavailable("problem service returned invalid judge limits")
	}
	revision := profile.GetActiveJudgeRevision()
	if len(revision) != 26 {
		return biz.JudgeProfile{}, biz.ErrorProblemUnavailable("problem has no published judge revision")
	}
	if _, err := ulid.ParseStrict(revision); err != nil {
		return biz.JudgeProfile{}, biz.ErrorDependencyUnavailable("problem service returned an invalid judge revision")
	}
	return biz.JudgeProfile{
		ProblemID: problemID, TimeLimitMS: profile.GetTimeLimitMs(), MemoryLimitKB: profile.GetMemoryLimitKb(), JudgeRevision: revision,
	}, nil
}

func resolveProblemActor(ctx context.Context) internalauth.Actor {
	principal, ok := internalauth.PrincipalFromContext(ctx)
	if !ok {
		return internalauth.Actor{}
	}
	return internalauth.Actor{
		ID: principal.ActorID, Roles: append([]string(nil), principal.ActorRoles...),
		RequestID: principal.RequestID, TraceID: principal.TraceID,
	}
}

func mapProblemError(err error) error {
	switch status.Code(err) {
	case codes.NotFound:
		return biz.ErrorProblemUnavailable("problem is not available for judging")
	case codes.FailedPrecondition:
		return biz.ErrorProblemUnavailable("problem is not available for judging")
	case codes.InvalidArgument:
		return biz.ErrorInvalidArgument("invalid problem id")
	case codes.DeadlineExceeded, codes.Unavailable, codes.ResourceExhausted:
		return biz.ErrorDependencyUnavailable("problem service is unavailable")
	default:
		return biz.ErrorDependencyUnavailable("problem service request failed")
	}
}

var _ biz.ProblemCatalog = (*ProblemClient)(nil)

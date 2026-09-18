package data

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"github.com/viggggil/go_oj_agent/pkg/internalauth"
	"github.com/viggggil/go_oj_agent/services/judge/internal/biz"
	"github.com/viggggil/go_oj_agent/services/judge/internal/conf"
)

type fakeProblemServiceClient struct {
	problemv1.ProblemServiceClient
	response *problemv1.GetJudgeProfileResponse
	err      error
	wantID   int64
}

func (f *fakeProblemServiceClient) GetJudgeProfile(_ context.Context, request *problemv1.GetJudgeProfileRequest, _ ...grpc.CallOption) (*problemv1.GetJudgeProfileResponse, error) {
	if f.wantID != 0 && request.GetProblemId() != f.wantID {
		return nil, errors.New("unexpected problem id")
	}
	return f.response, f.err
}

func TestProblemClientGetJudgeProfile(t *testing.T) {
	client := newProblemClient(&fakeProblemServiceClient{wantID: 7, response: &problemv1.GetJudgeProfileResponse{Profile: &problemv1.JudgeProfile{
		ProblemId: 7, Status: problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL,
		TimeLimitMs: 1000, MemoryLimitKb: 65536, ActiveJudgeRevision: dataTestRevision,
	}}})
	profile, err := client.GetJudgeProfile(context.Background(), 7)
	if err != nil {
		t.Fatalf("GetJudgeProfile() error = %v", err)
	}
	if profile.ProblemID != 7 || profile.JudgeRevision != dataTestRevision || profile.TimeLimitMS != 1000 || profile.MemoryLimitKB != 65536 {
		t.Fatalf("GetJudgeProfile() = %+v", profile)
	}
}

func TestProblemClientValidatesProfile(t *testing.T) {
	valid := &problemv1.JudgeProfile{
		ProblemId: 7, Status: problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL,
		TimeLimitMs: 1000, MemoryLimitKb: 65536, ActiveJudgeRevision: dataTestRevision,
	}
	tests := []struct {
		name   string
		mutate func(*problemv1.JudgeProfile)
		reason string
	}{
		{"id", func(p *problemv1.JudgeProfile) { p.ProblemId = 8 }, biz.ReasonDependencyUnavailable},
		{"status", func(p *problemv1.JudgeProfile) { p.Status = problemv1.ProblemStatus_PROBLEM_STATUS_ARCHIVED }, biz.ReasonProblemUnavailable},
		{"time", func(p *problemv1.JudgeProfile) { p.TimeLimitMs = 0 }, biz.ReasonDependencyUnavailable},
		{"memory", func(p *problemv1.JudgeProfile) { p.MemoryLimitKb = 0 }, biz.ReasonDependencyUnavailable},
		{"missing revision", func(p *problemv1.JudgeProfile) { p.ActiveJudgeRevision = "" }, biz.ReasonProblemUnavailable},
		{"malformed revision", func(p *problemv1.JudgeProfile) { p.ActiveJudgeRevision = strings.Repeat("x", 26) }, biz.ReasonDependencyUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profile := proto.Clone(valid).(*problemv1.JudgeProfile)
			test.mutate(profile)
			client := newProblemClient(&fakeProblemServiceClient{response: &problemv1.GetJudgeProfileResponse{Profile: profile}})
			_, err := client.GetJudgeProfile(context.Background(), 7)
			if !biz.HasReason(err, test.reason) {
				t.Fatalf("error = %v, want reason %s", err, test.reason)
			}
		})
	}
}

func TestProblemClientMapsRemoteErrors(t *testing.T) {
	tests := []struct {
		code   codes.Code
		reason string
	}{
		{codes.NotFound, biz.ReasonProblemUnavailable},
		{codes.FailedPrecondition, biz.ReasonProblemUnavailable},
		{codes.Unavailable, biz.ReasonDependencyUnavailable},
		{codes.DeadlineExceeded, biz.ReasonDependencyUnavailable},
		{codes.Internal, biz.ReasonDependencyUnavailable},
	}
	for _, test := range tests {
		client := newProblemClient(&fakeProblemServiceClient{err: status.Error(test.code, "remote details")})
		_, err := client.GetJudgeProfile(context.Background(), 7)
		if !biz.HasReason(err, test.reason) || strings.Contains(err.Error(), "remote details") {
			t.Errorf("code %s error = %v, want reason %s without remote details", test.code, err, test.reason)
		}
	}
}

func TestProblemAuthInterceptorSignsJudgeIdentityAndActor(t *testing.T) {
	privatePEM, publicPEM := generateRSAKeypair(t)
	now := time.Date(2026, 9, 18, 1, 2, 3, 0, time.UTC)
	cfg := &conf.ProblemClientProto{
		KeyId: "judge-internal-2026-09", Issuer: "go-oj-judge", Audience: "problem-service",
		Subject: "judge-service", TokenTtl: "30s",
	}
	interceptor, err := newProblemAuthInterceptor(cfg, privatePEM, func() time.Time { return now })
	if err != nil {
		t.Fatalf("newProblemAuthInterceptor() error = %v", err)
	}
	verifier, err := internalauth.NewVerifier(map[string][]byte{cfg.GetKeyId(): publicPEM}, cfg.GetIssuer(), cfg.GetAudience(), cfg.GetSubject(), time.Minute, 0, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewVerifier() error = %v", err)
	}
	ctx := internalauth.WithPrincipal(context.Background(), internalauth.Principal{
		ActorID: 42, ActorRoles: []string{"admin"}, RequestID: "request-1", TraceID: "trace-1",
	})
	method := problemv1.ProblemService_GetJudgeProfile_FullMethodName
	var claims internalauth.Claims
	err = interceptor(ctx, method, nil, nil, nil, func(callCtx context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
		outgoing, _ := metadata.FromOutgoingContext(callCtx)
		values := outgoing.Get("authorization")
		if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
			t.Fatalf("authorization metadata = %v", values)
		}
		var verifyErr error
		claims, verifyErr = verifier.Verify(strings.TrimPrefix(values[0], "Bearer "), method)
		return verifyErr
	})
	if err != nil {
		t.Fatalf("interceptor() error = %v", err)
	}
	if claims.Subject != "judge-service" || claims.ActorID != 42 || len(claims.ActorRoles) != 1 || claims.ActorRoles[0] != "admin" || claims.RequestID != "request-1" || claims.TraceID != "trace-1" {
		t.Fatalf("claims = %+v", claims)
	}
}

func generateRSAKeypair(t *testing.T) ([]byte, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}), pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER})
}

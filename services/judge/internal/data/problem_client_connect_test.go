package data

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"github.com/viggggil/go_oj_agent/services/judge/internal/conf"
)

// TestProblemClientConnectsWithoutPanic guards against regressions where the
// judge problem client goes through kratos' selector balancer, whose builder
// is nil when only transport/grpc is imported (see
// github.com/go-kratos/kratos/v3/transport/grpc/balancer.go). A real connection
// would then panic inside balancerBuilder.Build. The client must use plain grpc.
func TestProblemClientConnectsWithoutPanic(t *testing.T) {
	privatePEM, _ := generateRSAKeypair(t)
	keyFile := filepath.Join(t.TempDir(), "judge-private.pem")
	if err := os.WriteFile(keyFile, privatePEM, 0o600); err != nil {
		t.Fatalf("write private key: %v", err)
	}

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := grpc.NewServer()
	problemv1.RegisterProblemServiceServer(s, &problemv1.UnimplementedProblemServiceServer{})
	healthpb.RegisterHealthServer(s, health.NewServer())
	go s.Serve(lis)
	defer s.Stop()

	cfg := &conf.Bootstrap{
		Clients: &conf.ClientsProto{
			Problem: &conf.ProblemClientProto{
				Endpoint:       lis.Addr().String(),
				Timeout:        "3s",
				PrivateKeyFile: keyFile,
				KeyId:          "judge-internal-2026-09",
				Issuer:         "go-oj-judge",
				Audience:       "problem-service",
				Subject:        "judge-service",
				TokenTtl:       "30s",
			},
		},
	}

	client, cleanup, err := NewProblemClient(cfg)
	if err != nil {
		t.Fatalf("NewProblemClient: %v", err)
	}
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = client.GetJudgeProfile(ctx, 7)
	if err == nil {
		t.Fatal("expected error from fake server, got nil")
	}
}

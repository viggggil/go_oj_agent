package data

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"github.com/viggggil/go_oj_agent/pkg/internalauth"
	"github.com/viggggil/go_oj_agent/services/contest/internal/conf"
	"google.golang.org/grpc"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type batchProblemServer struct {
	problemv1.UnimplementedProblemServiceServer
	actor internalauth.Principal
	ids   []int64
}

func (s *batchProblemServer) BatchGetProblems(ctx context.Context, req *problemv1.BatchGetProblemsRequest) (*problemv1.BatchGetProblemsResponse, error) {
	s.actor, _ = internalauth.PrincipalFromContext(ctx)
	s.ids = req.GetProblemIds()
	return &problemv1.BatchGetProblemsResponse{Problems: []*problemv1.ProblemSummary{{Id: 7, Title: "Two Sum"}}}, nil
}
func TestProblemClientBatchPreservesActor(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	private := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})
	publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	public := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER})
	keyFile := filepath.Join(t.TempDir(), "private.pem")
	if err := os.WriteFile(keyFile, private, 0600); err != nil {
		t.Fatal(err)
	}
	verifier, err := internalauth.NewVerifier(map[string][]byte{"test-key": public}, "test-issuer", "problem-service", "gateway-service", time.Minute, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(grpc.UnaryInterceptor(internalauth.UnaryServerInterceptor(verifier)))
	fake := &batchProblemServer{}
	problemv1.RegisterProblemServiceServer(server, fake)
	go server.Serve(listener)
	defer server.Stop()
	cfg := &conf.Bootstrap{Problem: &conf.ClientProto{Endpoint: listener.Addr().String(), Timeout: "3s", PrivateKeyFile: keyFile, KeyId: "test-key", Issuer: "test-issuer", Audience: "problem-service", Subject: "gateway-service", TokenTtl: "30s"}}
	client, cleanup, err := NewProblemClient(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	ctx := internalauth.WithPrincipal(context.Background(), internalauth.Principal{ActorID: 42, ActorRoles: []string{"admin"}, RequestID: "req-42"})
	titles, err := client.Titles(ctx, []int64{7, 999})
	if err != nil {
		t.Fatal(err)
	}
	if titles[7] != "Two Sum" || fake.actor.ActorID != 42 || len(fake.actor.ActorRoles) != 1 || fake.actor.ActorRoles[0] != "admin" || fake.actor.RequestID != "req-42" || len(fake.ids) != 2 {
		t.Fatalf("titles=%v actor=%+v ids=%v", titles, fake.actor, fake.ids)
	}
}

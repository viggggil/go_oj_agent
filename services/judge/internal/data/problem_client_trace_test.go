package data

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"

	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"github.com/viggggil/go_oj_agent/services/judge/internal/conf"
)

type traceProblemServer struct {
	problemv1.UnimplementedProblemServiceServer
	spanContext trace.SpanContext
}

func (s *traceProblemServer) GetJudgeProfile(ctx context.Context, req *problemv1.GetJudgeProfileRequest) (*problemv1.GetJudgeProfileResponse, error) {
	s.spanContext = trace.SpanContextFromContext(ctx)
	return &problemv1.GetJudgeProfileResponse{Profile: &problemv1.JudgeProfile{
		ProblemId: req.GetProblemId(), Status: problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL,
		TimeLimitMs: 1000, MemoryLimitKb: 65536, ActiveJudgeRevision: "01K5C6Y7N8P9Q0R1S2T3V4W5X6",
	}}, nil
}

func TestProblemClientPropagatesTraceContext(t *testing.T) {
	previousProvider := otel.GetTracerProvider()
	previousPropagator := otel.GetTextMapPropagator()
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)))
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	defer func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(previousProvider)
		otel.SetTextMapPropagator(previousPropagator)
	}()

	privatePEM, _ := generateRSAKeypair(t)
	keyFile := filepath.Join(t.TempDir(), "judge-private.pem")
	if err := os.WriteFile(keyFile, privatePEM, 0o600); err != nil {
		t.Fatalf("write private key: %v", err)
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := &traceProblemServer{}
	grpcServer := grpc.NewServer(grpc.StatsHandler(otelgrpc.NewServerHandler()))
	problemv1.RegisterProblemServiceServer(grpcServer, server)
	go grpcServer.Serve(lis)
	defer grpcServer.Stop()

	client, cleanup, err := NewProblemClient(&conf.Bootstrap{Clients: &conf.ClientsProto{Problem: &conf.ProblemClientProto{
		Endpoint: lis.Addr().String(), Timeout: "3s", PrivateKeyFile: keyFile,
		KeyId: "judge-internal-2026-09", Issuer: "go-oj-judge", Audience: "problem-service",
		Subject: "judge-service", TokenTtl: "30s",
	}}})
	if err != nil {
		t.Fatalf("NewProblemClient: %v", err)
	}
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rootCtx, root := otel.Tracer("test").Start(ctx, "root")
	if _, err := client.GetJudgeProfile(rootCtx, 7); err != nil {
		t.Fatalf("GetJudgeProfile: %v", err)
	}
	root.End()

	if !server.spanContext.IsValid() || server.spanContext.TraceID() != trace.SpanContextFromContext(rootCtx).TraceID() {
		t.Fatalf("server did not receive parent trace context: %+v", server.spanContext)
	}
	spans := exporter.GetSpans()
	var serverSpan bool
	for _, span := range spans {
		if span.SpanKind == trace.SpanKindServer {
			serverSpan = true
			break
		}
	}
	if !serverSpan {
		t.Fatal("expected otelgrpc server span")
	}
}

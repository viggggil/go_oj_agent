package service

import (
	"context"
	"testing"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	gatewayv1 "github.com/viggggil/go_oj_agent/api/gateway/v1"
	submissionv1 "github.com/viggggil/go_oj_agent/api/submission/v1"
	gatewaymw "github.com/viggggil/go_oj_agent/services/gateway/internal/middleware"
	"google.golang.org/grpc"
)

type fakeSubmissionClient struct {
	create *submissionv1.CreateSubmissionRequest
	result *submissionv1.GetJudgeResultResponse
}

func (f *fakeSubmissionClient) CreateSubmission(_ context.Context, req *submissionv1.CreateSubmissionRequest, _ ...grpc.CallOption) (*submissionv1.CreateSubmissionResponse, error) {
	f.create = req
	return &submissionv1.CreateSubmissionResponse{SubmissionId: 9, Status: submissionv1.SubmissionStatus_SUBMISSION_STATUS_QUEUED}, nil
}
func (f *fakeSubmissionClient) GetSubmission(context.Context, *submissionv1.GetSubmissionRequest, ...grpc.CallOption) (*submissionv1.GetSubmissionResponse, error) {
	return &submissionv1.GetSubmissionResponse{}, nil
}
func (f *fakeSubmissionClient) ListSubmissions(context.Context, *submissionv1.ListSubmissionsRequest, ...grpc.CallOption) (*submissionv1.ListSubmissionsResponse, error) {
	return &submissionv1.ListSubmissionsResponse{}, nil
}
func (f *fakeSubmissionClient) GetJudgeResult(context.Context, *submissionv1.GetJudgeResultRequest, ...grpc.CallOption) (*submissionv1.GetJudgeResultResponse, error) {
	return f.result, nil
}
func (f *fakeSubmissionClient) RejudgeSubmission(context.Context, *submissionv1.RejudgeSubmissionRequest, ...grpc.CallOption) (*submissionv1.RejudgeSubmissionResponse, error) {
	return &submissionv1.RejudgeSubmissionResponse{}, nil
}

func TestCreateSubmissionForwardsRequestContextAndFields(t *testing.T) {
	fake := &fakeSubmissionClient{}
	s := &GatewayService{submission: fake}
	ctx := gatewaymw.WithRequestContext(context.Background(), &commonv1.RequestContext{UserId: 42, RequestId: "req-1", TraceId: "trace-1"})
	response, err := s.CreateSubmission(ctx, &gatewayv1.CreateSubmissionRequest{ProblemId: 7, Language: "go", SourceCode: "package main", IdempotencyKey: "550e8400-e29b-41d4-a716-446655440000"})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetSubmissionId() != 9 || fake.create.GetProblemId() != 7 || fake.create.GetIdempotencyKey() == "" {
		t.Fatalf("response=%+v request=%+v", response, fake.create)
	}
}

func TestSubmissionRequiresRequestContext(t *testing.T) {
	_, err := (&GatewayService{submission: &fakeSubmissionClient{}}).GetJudgeResult(context.Background(), &gatewayv1.GetJudgeResultRequest{SubmissionId: 1})
	if err == nil {
		t.Fatal("expected unauthenticated error")
	}
}

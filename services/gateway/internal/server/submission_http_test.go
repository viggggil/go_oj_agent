package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	submissionv1 "github.com/viggggil/go_oj_agent/api/submission/v1"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/middleware"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/service"
	"google.golang.org/grpc"
)

type httpSubmissionClient struct {
	create *submissionv1.CreateSubmissionRequest
	result *submissionv1.GetJudgeResultResponse
}

func (f *httpSubmissionClient) CreateSubmission(_ context.Context, req *submissionv1.CreateSubmissionRequest, _ ...grpc.CallOption) (*submissionv1.CreateSubmissionResponse, error) {
	f.create = req
	return &submissionv1.CreateSubmissionResponse{SubmissionId: 9, Status: submissionv1.SubmissionStatus_SUBMISSION_STATUS_QUEUED}, nil
}
func (f *httpSubmissionClient) GetSubmission(context.Context, *submissionv1.GetSubmissionRequest, ...grpc.CallOption) (*submissionv1.GetSubmissionResponse, error) {
	return &submissionv1.GetSubmissionResponse{}, nil
}
func (f *httpSubmissionClient) ListSubmissions(context.Context, *submissionv1.ListSubmissionsRequest, ...grpc.CallOption) (*submissionv1.ListSubmissionsResponse, error) {
	return &submissionv1.ListSubmissionsResponse{}, nil
}
func (f *httpSubmissionClient) GetJudgeResult(context.Context, *submissionv1.GetJudgeResultRequest, ...grpc.CallOption) (*submissionv1.GetJudgeResultResponse, error) {
	return f.result, nil
}
func (f *httpSubmissionClient) RejudgeSubmission(context.Context, *submissionv1.RejudgeSubmissionRequest, ...grpc.CallOption) (*submissionv1.RejudgeSubmissionResponse, error) {
	return &submissionv1.RejudgeSubmissionResponse{}, nil
}

func TestSubmissionHTTPAndSSE(t *testing.T) {
	client := &httpSubmissionClient{result: &submissionv1.GetJudgeResultResponse{Result: &submissionv1.JudgeResult{SubmissionId: 9, Status: submissionv1.SubmissionStatus_SUBMISSION_STATUS_DONE, Verdict: submissionv1.JudgeVerdict_JUDGE_VERDICT_AC}}}
	auth, err := middleware.NewAuthMiddleware(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	h := NewHTTPServer(testConfig(), auth, service.NewGatewayServiceWithClients(service.NewAuthService(&fakeUserClient{}), service.NewUserService(&fakeUserClient{}), nil, client))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/submissions", strings.NewReader(`{"problem_id":7,"language":"go","source_code":"package main","idempotency_key":"550e8400-e29b-41d4-a716-446655440000"}`))
	request.Header.Set("Authorization", "Bearer "+testAccessToken(t))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusOK || client.create.GetProblemId() != 7 {
		t.Fatalf("create status=%d body=%s request=%+v", response.Code, response.Body.String(), client.create)
	}

	sseRequest := httptest.NewRequest(http.MethodGet, "/api/v1/submissions/9/events", nil)
	sseRequest.Header.Set("Authorization", "Bearer "+testAccessToken(t))
	sseResponse := httptest.NewRecorder()
	h.ServeHTTP(sseResponse, sseRequest)
	if sseResponse.Code != http.StatusOK || !strings.Contains(sseResponse.Header().Get("Content-Type"), "text/event-stream") || !strings.Contains(sseResponse.Body.String(), "submission.snapshot") {
		t.Fatalf("sse status=%d content-type=%q body=%s", sseResponse.Code, sseResponse.Header().Get("Content-Type"), sseResponse.Body.String())
	}
}

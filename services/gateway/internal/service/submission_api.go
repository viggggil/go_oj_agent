package service

import (
	"context"
	"fmt"

	gatewayv1 "github.com/viggggil/go_oj_agent/api/gateway/v1"
	submissionv1 "github.com/viggggil/go_oj_agent/api/submission/v1"
	gatewaymw "github.com/viggggil/go_oj_agent/services/gateway/internal/middleware"
)

func (s *GatewayService) submissionClient(ctx context.Context) (submissionv1.SubmissionServiceClient, error) {
	if _, ok := gatewaymw.RequestContextFromContext(ctx); !ok {
		return nil, gatewaymw.ErrUnauthenticated("request context is missing")
	}
	if s == nil || s.submission == nil {
		return nil, fmt.Errorf("gateway submission service is not configured")
	}
	return s.submission, nil
}

func (s *GatewayService) CreateSubmission(ctx context.Context, req *gatewayv1.CreateSubmissionRequest) (*gatewayv1.CreateSubmissionResponse, error) {
	client, err := s.submissionClient(ctx)
	if err != nil {
		return nil, err
	}
	res, err := client.CreateSubmission(ctx, &submissionv1.CreateSubmissionRequest{ProblemId: req.GetProblemId(), Language: req.GetLanguage(), SourceCode: req.GetSourceCode(), IdempotencyKey: req.GetIdempotencyKey()})
	if err != nil {
		return nil, err
	}
	return &gatewayv1.CreateSubmissionResponse{SubmissionId: res.GetSubmissionId(), Status: res.GetStatus()}, nil
}

func (s *GatewayService) GetSubmission(ctx context.Context, req *gatewayv1.GetSubmissionRequest) (*gatewayv1.GetSubmissionResponse, error) {
	client, err := s.submissionClient(ctx)
	if err != nil {
		return nil, err
	}
	res, err := client.GetSubmission(ctx, &submissionv1.GetSubmissionRequest{SubmissionId: req.GetSubmissionId()})
	if err != nil {
		return nil, err
	}
	return &gatewayv1.GetSubmissionResponse{Submission: res.GetSubmission()}, nil
}

func (s *GatewayService) GetJudgeResult(ctx context.Context, req *gatewayv1.GetJudgeResultRequest) (*gatewayv1.GetJudgeResultResponse, error) {
	client, err := s.submissionClient(ctx)
	if err != nil {
		return nil, err
	}
	res, err := client.GetJudgeResult(ctx, &submissionv1.GetJudgeResultRequest{SubmissionId: req.GetSubmissionId()})
	if err != nil {
		return nil, err
	}
	return &gatewayv1.GetJudgeResultResponse{Result: res.GetResult()}, nil
}

func (s *GatewayService) ListSubmissions(ctx context.Context, req *gatewayv1.ListSubmissionsRequest) (*gatewayv1.ListSubmissionsResponse, error) {
	client, err := s.submissionClient(ctx)
	if err != nil {
		return nil, err
	}
	res, err := client.ListSubmissions(ctx, &submissionv1.ListSubmissionsRequest{Page: req.GetPage(), ProblemId: req.GetProblemId(), Status: req.GetStatus(), Language: req.GetLanguage(), UserId: req.GetUserId()})
	if err != nil {
		return nil, err
	}
	return &gatewayv1.ListSubmissionsResponse{Items: res.GetItems(), Page: res.GetPage()}, nil
}

func (s *GatewayService) RejudgeSubmission(ctx context.Context, req *gatewayv1.RejudgeSubmissionRequest) (*gatewayv1.RejudgeSubmissionResponse, error) {
	client, err := s.submissionClient(ctx)
	if err != nil {
		return nil, err
	}
	res, err := client.RejudgeSubmission(ctx, &submissionv1.RejudgeSubmissionRequest{SubmissionId: req.GetSubmissionId(), IdempotencyKey: req.GetIdempotencyKey()})
	if err != nil {
		return nil, err
	}
	return &gatewayv1.RejudgeSubmissionResponse{InvalidatedSubmissionId: res.GetInvalidatedSubmissionId(), Submission: res.GetSubmission()}, nil
}

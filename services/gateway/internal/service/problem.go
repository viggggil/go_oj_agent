package service

import (
	"context"
	"fmt"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	gatewayv1 "github.com/viggggil/go_oj_agent/api/gateway/v1"
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	gatewaymw "github.com/viggggil/go_oj_agent/services/gateway/internal/middleware"
)

func (s *GatewayService) problemClient(ctx context.Context) (problemv1.ProblemServiceClient, *commonv1.RequestContext, error) {
	requestContext, ok := gatewaymw.RequestContextFromContext(ctx)
	if !ok {
		return nil, nil, gatewaymw.ErrUnauthenticated("request context is missing")
	}
	if s == nil || s.problem == nil {
		return nil, nil, fmt.Errorf("gateway problem service is not configured")
	}
	return s.problem, requestContext, nil
}

func (s *GatewayService) CreateProblem(ctx context.Context, req *gatewayv1.CreateProblemRequest) (*gatewayv1.CreateProblemResponse, error) {
	client, requestContext, err := s.problemClient(ctx)
	if err != nil {
		return nil, err
	}
	response, err := client.CreateProblem(ctx, &problemv1.CreateProblemRequest{Context: requestContext, Problem: req.GetProblem(), Testcases: req.GetTestcases()})
	if err != nil {
		return nil, err
	}
	return &gatewayv1.CreateProblemResponse{Problem: response.GetProblem()}, nil
}

func (s *GatewayService) GetProblem(ctx context.Context, req *gatewayv1.GetProblemRequest) (*gatewayv1.GetProblemResponse, error) {
	client, requestContext, err := s.problemClient(ctx)
	if err != nil {
		return nil, err
	}
	response, err := client.GetProblem(ctx, &problemv1.GetProblemRequest{Context: requestContext, ProblemId: req.GetProblemId()})
	if err != nil {
		return nil, err
	}
	return &gatewayv1.GetProblemResponse{Problem: response.GetProblem()}, nil
}

func (s *GatewayService) ListProblems(ctx context.Context, req *gatewayv1.ListProblemsRequest) (*gatewayv1.ListProblemsResponse, error) {
	client, requestContext, err := s.problemClient(ctx)
	if err != nil {
		return nil, err
	}
	response, err := client.ListProblems(ctx, &problemv1.ListProblemsRequest{Context: requestContext, Page: req.GetPage()})
	if err != nil {
		return nil, err
	}
	return &gatewayv1.ListProblemsResponse{Items: response.GetItems(), Page: response.GetPage()}, nil
}

func (s *GatewayService) UpdateProblem(ctx context.Context, req *gatewayv1.UpdateProblemRequest) (*gatewayv1.UpdateProblemResponse, error) {
	client, requestContext, err := s.problemClient(ctx)
	if err != nil {
		return nil, err
	}
	response, err := client.UpdateProblem(ctx, &problemv1.UpdateProblemRequest{Context: requestContext, ProblemId: req.GetProblemId(), Problem: req.GetProblem()})
	if err != nil {
		return nil, err
	}
	return &gatewayv1.UpdateProblemResponse{Problem: response.GetProblem()}, nil
}

func (s *GatewayService) ArchiveProblem(ctx context.Context, req *gatewayv1.ArchiveProblemRequest) (*gatewayv1.ArchiveProblemResponse, error) {
	client, requestContext, err := s.problemClient(ctx)
	if err != nil {
		return nil, err
	}
	response, err := client.ArchiveProblem(ctx, &problemv1.ArchiveProblemRequest{Context: requestContext, ProblemId: req.GetProblemId()})
	if err != nil {
		return nil, err
	}
	return &gatewayv1.ArchiveProblemResponse{Problem: response.GetProblem()}, nil
}

func (s *GatewayService) AddTestcase(ctx context.Context, req *gatewayv1.AddTestcaseRequest) (*gatewayv1.AddTestcaseResponse, error) {
	client, requestContext, err := s.problemClient(ctx)
	if err != nil {
		return nil, err
	}
	response, err := client.AddTestcase(ctx, &problemv1.AddTestcaseRequest{Context: requestContext, ProblemId: req.GetProblemId(), CaseNo: req.GetCaseNo(), InputFilename: req.GetInputFilename(), InputContent: req.GetInputContent(), OutputFilename: req.GetOutputFilename(), OutputContent: req.GetOutputContent()})
	if err != nil {
		return nil, err
	}
	return &gatewayv1.AddTestcaseResponse{Testcase: response.GetTestcase()}, nil
}

func (s *GatewayService) ListProblemTestcases(ctx context.Context, req *gatewayv1.ListProblemTestcasesRequest) (*gatewayv1.ListProblemTestcasesResponse, error) {
	client, requestContext, err := s.problemClient(ctx)
	if err != nil {
		return nil, err
	}
	response, err := client.ListProblemTestcases(ctx, &problemv1.ListProblemTestcasesRequest{Context: requestContext, ProblemId: req.GetProblemId(), IncludeArchived: req.GetIncludeArchived()})
	if err != nil {
		return nil, err
	}
	return &gatewayv1.ListProblemTestcasesResponse{Items: response.GetItems()}, nil
}

func (s *GatewayService) ArchiveTestcase(ctx context.Context, req *gatewayv1.ArchiveTestcaseRequest) (*gatewayv1.ArchiveTestcaseResponse, error) {
	client, requestContext, err := s.problemClient(ctx)
	if err != nil {
		return nil, err
	}
	response, err := client.ArchiveTestcase(ctx, &problemv1.ArchiveTestcaseRequest{Context: requestContext, ProblemId: req.GetProblemId(), TestcaseId: req.GetTestcaseId()})
	if err != nil {
		return nil, err
	}
	return &gatewayv1.ArchiveTestcaseResponse{Testcase: response.GetTestcase()}, nil
}

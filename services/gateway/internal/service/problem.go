package service

import (
	"context"
	"fmt"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	gatewaymw "github.com/viggggil/go_oj_agent/services/gateway/internal/middleware"
)

func (s *GatewayService) rc(ctx context.Context) (problemv1.ProblemServiceClient, *commonv1.RequestContext, error) {
	rc, ok := gatewaymw.RequestContextFromContext(ctx)
	if !ok {
		return nil, nil, gatewaymw.ErrUnauthenticated("request context is missing")
	}
	if s == nil || s.problem == nil {
		return nil, nil, fmt.Errorf("gateway problem service is not configured")
	}
	return s.problem, rc, nil
}
func (s *GatewayService) CreateProblem(ctx context.Context, r *problemv1.CreateProblemRequest) (*problemv1.CreateProblemResponse, error) {
	c, rc, e := s.rc(ctx)
	if e != nil {
		return nil, e
	}
	r.Context = rc
	return c.CreateProblem(ctx, r)
}
func (s *GatewayService) GetProblem(ctx context.Context, r *problemv1.GetProblemRequest) (*problemv1.GetProblemResponse, error) {
	c, rc, e := s.rc(ctx)
	if e != nil {
		return nil, e
	}
	r.Context = rc
	return c.GetProblem(ctx, r)
}
func (s *GatewayService) ListProblems(ctx context.Context, r *problemv1.ListProblemsRequest) (*problemv1.ListProblemsResponse, error) {
	c, rc, e := s.rc(ctx)
	if e != nil {
		return nil, e
	}
	r.Context = rc
	return c.ListProblems(ctx, r)
}
func (s *GatewayService) UpdateProblem(ctx context.Context, r *problemv1.UpdateProblemRequest) (*problemv1.UpdateProblemResponse, error) {
	c, rc, e := s.rc(ctx)
	if e != nil {
		return nil, e
	}
	r.Context = rc
	return c.UpdateProblem(ctx, r)
}
func (s *GatewayService) ArchiveProblem(ctx context.Context, r *problemv1.ArchiveProblemRequest) (*problemv1.ArchiveProblemResponse, error) {
	c, rc, e := s.rc(ctx)
	if e != nil {
		return nil, e
	}
	r.Context = rc
	return c.ArchiveProblem(ctx, r)
}
func (s *GatewayService) AddTestcase(ctx context.Context, r *problemv1.AddTestcaseRequest) (*problemv1.AddTestcaseResponse, error) {
	c, rc, e := s.rc(ctx)
	if e != nil {
		return nil, e
	}
	r.Context = rc
	return c.AddTestcase(ctx, r)
}
func (s *GatewayService) ListProblemTestcases(ctx context.Context, r *problemv1.ListProblemTestcasesRequest) (*problemv1.ListProblemTestcasesResponse, error) {
	c, rc, e := s.rc(ctx)
	if e != nil {
		return nil, e
	}
	r.Context = rc
	return c.ListProblemTestcases(ctx, r)
}
func (s *GatewayService) ArchiveTestcase(ctx context.Context, r *problemv1.ArchiveTestcaseRequest) (*problemv1.ArchiveTestcaseResponse, error) {
	c, rc, e := s.rc(ctx)
	if e != nil {
		return nil, e
	}
	r.Context = rc
	return c.ArchiveTestcase(ctx, r)
}

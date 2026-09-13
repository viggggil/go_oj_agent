package service

import (
	"context"
	"github.com/google/wire"

	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"github.com/viggggil/go_oj_agent/services/problem/internal/biz"
)

var ProviderSet = wire.NewSet(NewProblemService)

const Name = "problem-service"

// ProblemService exposes the generated gRPC contract. Endpoint behavior is
// implemented as vertical slices after the service runtime is established.
type ProblemService struct {
	problemv1.UnimplementedProblemServiceServer
	uc *biz.ProblemUsecase
}

func NewProblemService(uc *biz.ProblemUsecase) *ProblemService {
	return &ProblemService{uc: uc}
}

func (s *ProblemService) CreateProblem(ctx context.Context, req *problemv1.CreateProblemRequest) (*problemv1.CreateProblemResponse, error) {
	if req == nil || s == nil || s.uc == nil {
		return nil, biz.ErrorInvalidArgument("invalid create problem request")
	}
	if err := req.Validate(); err != nil {
		return nil, biz.ErrorInvalidArgument("%s", err.Error())
	}
	in := req.GetProblem()
	p, err := s.uc.Create(ctx, biz.CreateProblemInput{Context: req.GetContext(), Title: in.GetTitle(), Slug: in.GetSlug(), Description: in.GetDescription(), Difficulty: in.GetDifficulty(), TimeLimitMs: in.GetTimeLimitMs(), MemoryLimitKb: in.GetMemoryLimitKb(), Tags: in.GetTags()})
	if err != nil {
		return nil, err
	}
	return &problemv1.CreateProblemResponse{Problem: &problemv1.Problem{Id: p.ID, Title: p.Title, Slug: p.Slug, Description: p.Description, Difficulty: p.Difficulty, TimeLimitMs: p.TimeLimitMs, MemoryLimitKb: p.MemoryLimitKb, Status: p.Status, CreatedBy: p.CreatedBy}}, nil
}

package service

import (
	"context"
	"fmt"
	"github.com/google/wire"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
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
	for _, testcase := range req.GetTestcases() {
		if err := validateTestcasePair(testcase.GetCaseNo(), testcase.GetInputFilename(), testcase.GetOutputFilename()); err != nil {
			return nil, err
		}
	}
	in := req.GetProblem()
	p, err := s.uc.Create(ctx, biz.CreateProblemInput{
		Context: req.GetContext(),
		Problem: biz.Problem{
			Title:         in.GetTitle(),
			Slug:          in.GetSlug(),
			Description:   in.GetDescription(),
			Difficulty:    in.GetDifficulty(),
			TimeLimitMs:   in.GetTimeLimitMs(),
			MemoryLimitKb: in.GetMemoryLimitKb(),
		},
		Tags:      in.GetTags(),
		Testcases: toBizTestcaseContents(req.GetTestcases()),
	})
	if err != nil {
		return nil, err
	}
	return &problemv1.CreateProblemResponse{Problem: toProtoProblem(p)}, nil
}

func (s *ProblemService) GetProblem(ctx context.Context, req *problemv1.GetProblemRequest) (*problemv1.GetProblemResponse, error) {
	if req == nil || s == nil || s.uc == nil {
		return nil, biz.ErrorInvalidArgument("invalid get problem request")
	}
	if err := req.Validate(); err != nil {
		return nil, biz.ErrorInvalidArgument("%s", err.Error())
	}
	problem, err := s.uc.Get(ctx, req.GetContext(), req.GetProblemId())
	if err != nil {
		return nil, err
	}
	return &problemv1.GetProblemResponse{Problem: toProtoProblem(problem)}, nil
}

func (s *ProblemService) ListProblems(ctx context.Context, req *problemv1.ListProblemsRequest) (*problemv1.ListProblemsResponse, error) {
	if req == nil || s == nil || s.uc == nil {
		return nil, biz.ErrorInvalidArgument("invalid list problems request")
	}
	if err := req.Validate(); err != nil {
		return nil, biz.ErrorInvalidArgument("%s", err.Error())
	}
	page, err := s.uc.List(ctx, req.GetContext(), req.GetPage().GetPage(), req.GetPage().GetPageSize())
	if err != nil {
		return nil, err
	}
	items := make([]*problemv1.ProblemSummary, 0, len(page.Items))
	for _, problem := range page.Items {
		items = append(items, &problemv1.ProblemSummary{Id: problem.ID, Title: problem.Title, Slug: problem.Slug, Difficulty: problem.Difficulty, Status: problem.Status})
	}
	return &problemv1.ListProblemsResponse{Items: items, Page: &commonv1.PageResponse{Page: page.Page, PageSize: page.PageSize, Total: page.Total}}, nil
}

func (s *ProblemService) UpdateProblem(ctx context.Context, req *problemv1.UpdateProblemRequest) (*problemv1.UpdateProblemResponse, error) {
	if req == nil || s == nil || s.uc == nil {
		return nil, biz.ErrorInvalidArgument("invalid update problem request")
	}
	if err := req.Validate(); err != nil {
		return nil, biz.ErrorInvalidArgument("%s", err.Error())
	}
	in := req.GetProblem()
	problem, err := s.uc.Update(ctx, req.GetContext(), req.GetProblemId(), biz.Problem{
		Title: in.GetTitle(), Description: in.GetDescription(), Slug: in.GetSlug(), Difficulty: in.GetDifficulty(),
		TimeLimitMs: in.GetTimeLimitMs(), MemoryLimitKb: in.GetMemoryLimitKb(),
	}, in.GetTags())
	if err != nil {
		return nil, err
	}
	return &problemv1.UpdateProblemResponse{Problem: toProtoProblem(problem)}, nil
}

func (s *ProblemService) ArchiveProblem(ctx context.Context, req *problemv1.ArchiveProblemRequest) (*problemv1.ArchiveProblemResponse, error) {
	if req == nil || s == nil || s.uc == nil {
		return nil, biz.ErrorInvalidArgument("invalid archive problem request")
	}
	if err := req.Validate(); err != nil {
		return nil, biz.ErrorInvalidArgument("%s", err.Error())
	}
	problem, err := s.uc.Archive(ctx, req.GetContext(), req.GetProblemId())
	if err != nil {
		return nil, err
	}
	return &problemv1.ArchiveProblemResponse{Problem: toProtoProblem(problem)}, nil
}

func (s *ProblemService) AddTestcase(ctx context.Context, req *problemv1.AddTestcaseRequest) (*problemv1.AddTestcaseResponse, error) {
	if req == nil || s == nil || s.uc == nil {
		return nil, biz.ErrorInvalidArgument("invalid add testcase request")
	}
	if err := req.Validate(); err != nil {
		return nil, biz.ErrorInvalidArgument("%s", err.Error())
	}
	if err := validateTestcasePair(req.GetCaseNo(), req.GetInputFilename(), req.GetOutputFilename()); err != nil {
		return nil, err
	}
	testcase, err := s.uc.AddTestcase(ctx, req.GetContext(), req.GetProblemId(), req.GetCaseNo(), req.GetInputContent(), req.GetOutputContent())
	if err != nil {
		return nil, err
	}
	return &problemv1.AddTestcaseResponse{Testcase: toProtoTestcase(testcase)}, nil
}

func validateTestcasePair(caseNo int32, inputFilename, outputFilename string) error {
	wantInput := fmt.Sprintf("%d.in", caseNo)
	wantOutput := fmt.Sprintf("%d.out", caseNo)
	if inputFilename != wantInput || outputFilename != wantOutput {
		return biz.ErrorInvalidArgument("testcase files must be named %s and %s", wantInput, wantOutput)
	}
	return nil
}

func (s *ProblemService) ListProblemTestcases(ctx context.Context, req *problemv1.ListProblemTestcasesRequest) (*problemv1.ListProblemTestcasesResponse, error) {
	if req == nil || s == nil || s.uc == nil {
		return nil, biz.ErrorInvalidArgument("invalid list testcases request")
	}
	if err := req.Validate(); err != nil {
		return nil, biz.ErrorInvalidArgument("%s", err.Error())
	}
	items, err := s.uc.ListTestcases(ctx, req.GetContext(), req.GetProblemId(), req.GetIncludeArchived())
	if err != nil {
		return nil, err
	}
	response := make([]*problemv1.TestcaseMetadata, 0, len(items))
	for _, item := range items {
		response = append(response, toProtoTestcase(item))
	}
	return &problemv1.ListProblemTestcasesResponse{Items: response}, nil
}

func (s *ProblemService) ArchiveTestcase(ctx context.Context, req *problemv1.ArchiveTestcaseRequest) (*problemv1.ArchiveTestcaseResponse, error) {
	if req == nil || s == nil || s.uc == nil {
		return nil, biz.ErrorInvalidArgument("invalid archive testcase request")
	}
	if err := req.Validate(); err != nil {
		return nil, biz.ErrorInvalidArgument("%s", err.Error())
	}
	item, err := s.uc.ArchiveTestcase(ctx, req.GetContext(), req.GetProblemId(), req.GetTestcaseId())
	if err != nil {
		return nil, err
	}
	return &problemv1.ArchiveTestcaseResponse{Testcase: toProtoTestcase(item)}, nil
}

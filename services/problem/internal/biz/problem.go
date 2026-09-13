package biz

import (
	"context"
	"strings"
	"time"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
)

const RoleAdmin = "admin"

type Problem struct {
	ID            int64
	Title         string
	Slug          string
	Description   string
	Difficulty    problemv1.ProblemDifficulty
	TimeLimitMs   int32
	MemoryLimitKb int32
	Status        problemv1.ProblemStatus
	CreatedBy     int64
	Tags          []Tag
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type Tag struct {
	ID   int64
	Name string
}

type CreateProblemInput struct {
	Context      *commonv1.RequestContext
	Problem      Problem
	Tags         []string
	HasTestcases bool
}

type ProblemRepository interface {
	Create(context.Context, Problem, []string) (Problem, error)
	FindByID(context.Context, int64) (Problem, error)
}

func (uc *ProblemUsecase) Get(ctx context.Context, requestContext *commonv1.RequestContext, problemID int64) (Problem, error) {
	if uc == nil || uc.repo == nil {
		return Problem{}, ErrorInternal("problem repository is not configured")
	}
	if requestContext == nil || requestContext.GetUserId() <= 0 || problemID <= 0 {
		return Problem{}, ErrorInvalidArgument("invalid get problem request")
	}
	problem, err := uc.repo.FindByID(ctx, problemID)
	if err != nil {
		return Problem{}, err
	}
	if problem.Status == problemv1.ProblemStatus_PROBLEM_STATUS_ARCHIVED {
		if err := requireAdmin(requestContext); err != nil {
			return Problem{}, ErrorNotFound("problem not found")
		}
	}
	return problem, nil
}

type ProblemUsecase struct {
	repo ProblemRepository
}

func NewProblemUsecase(repo ProblemRepository) *ProblemUsecase {
	return &ProblemUsecase{repo: repo}
}

func requireAdmin(ctx *commonv1.RequestContext) error {
	if ctx == nil || ctx.GetUserId() <= 0 {
		return ErrorInvalidArgument("invalid request context")
	}
	for _, role := range ctx.GetRoles() {
		if strings.EqualFold(strings.TrimSpace(role), RoleAdmin) {
			return nil
		}
	}
	return ErrorPermissionDenied("admin role required")
}

func normalizeTags(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	tags := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		tags = append(tags, value)
	}
	return tags
}

func (uc *ProblemUsecase) Create(ctx context.Context, input CreateProblemInput) (Problem, error) {
	if uc == nil || uc.repo == nil {
		return Problem{}, ErrorInternal("problem repository is not configured")
	}
	if err := requireAdmin(input.Context); err != nil {
		return Problem{}, err
	}
	if input.HasTestcases {
		return Problem{}, ErrorInvalidStatus("testcase storage is not available yet")
	}

	problem := input.Problem
	problem.Title = strings.TrimSpace(problem.Title)
	problem.Slug = strings.TrimSpace(problem.Slug)
	problem.Description = strings.TrimSpace(problem.Description)
	problem.Status = problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL
	problem.CreatedBy = input.Context.GetUserId()
	return uc.repo.Create(ctx, problem, normalizeTags(input.Tags))
}

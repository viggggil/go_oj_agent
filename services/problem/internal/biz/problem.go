package biz

import (
	"context"
	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"strings"
)

type Problem struct {
	ID                         int64
	Title, Slug, Description   string
	Difficulty                 problemv1.ProblemDifficulty
	TimeLimitMs, MemoryLimitKb int32
	Status                     problemv1.ProblemStatus
	CreatedBy                  int64
	Tags                       []Tag
}
type Tag struct {
	ID   int64
	Name string
}
type CreateProblemInput struct {
	Context                    *commonv1.RequestContext
	Title, Slug, Description   string
	Difficulty                 problemv1.ProblemDifficulty
	TimeLimitMs, MemoryLimitKb int32
	Tags                       []string
}
type ProblemRepository interface {
	Create(context.Context, Problem, []string) (Problem, error)
}
type ProblemUsecase struct{ repo ProblemRepository }

func NewProblemUsecase(r ProblemRepository) *ProblemUsecase { return &ProblemUsecase{repo: r} }
func (u *ProblemUsecase) Create(ctx context.Context, in CreateProblemInput) (Problem, error) {
	if u == nil || u.repo == nil || in.Context == nil || in.Context.GetUserId() <= 0 {
		return Problem{}, ErrorInvalidArgument("invalid context")
	}
	admin := false
	for _, r := range in.Context.GetRoles() {
		admin = admin || strings.EqualFold(r, "admin")
	}
	if !admin {
		return Problem{}, ErrorPermissionDenied("admin role required")
	}
	if strings.TrimSpace(in.Title) == "" || strings.TrimSpace(in.Slug) == "" || strings.TrimSpace(in.Description) == "" || in.Difficulty == 0 || in.TimeLimitMs <= 0 || in.MemoryLimitKb <= 0 {
		return Problem{}, ErrorInvalidArgument("invalid problem input")
	}
	seen := map[string]bool{}
	tags := []string{}
	for _, t := range in.Tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" && !seen[t] {
			seen[t] = true
			tags = append(tags, t)
		}
	}
	return u.repo.Create(ctx, Problem{Title: strings.TrimSpace(in.Title), Slug: strings.TrimSpace(in.Slug), Description: strings.TrimSpace(in.Description), Difficulty: in.Difficulty, TimeLimitMs: in.TimeLimitMs, MemoryLimitKb: in.MemoryLimitKb, Status: problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL, CreatedBy: in.Context.GetUserId()}, tags)
}

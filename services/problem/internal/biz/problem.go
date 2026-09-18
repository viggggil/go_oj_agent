package biz

import (
	"context"
	"strings"
	"sync"
	"time"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
)

const RoleAdmin = "admin"

type Problem struct {
	ID                  int64
	Title               string
	Slug                string
	Description         string
	Difficulty          problemv1.ProblemDifficulty
	TimeLimitMs         int32
	MemoryLimitKb       int32
	ActiveJudgeRevision string
	Status              problemv1.ProblemStatus
	CreatedBy           int64
	Tags                []Tag
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type Tag struct {
	ID   int64
	Name string
}

func (uc *ProblemUsecase) GetJudgeProfile(ctx context.Context, problemID int64) (Problem, error) {
	if uc == nil || uc.repo == nil || uc.testcases == nil {
		return Problem{}, ErrorInternal("judge profile dependencies are not configured")
	}
	if problemID <= 0 {
		return Problem{}, ErrorInvalidArgument("invalid problem id")
	}
	unlock := uc.lockProblem(problemID)
	defer unlock()
	problem, err := uc.repo.FindByID(ctx, problemID)
	if err != nil {
		return Problem{}, err
	}
	if problem.Status != problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL {
		return Problem{}, ErrorInvalidStatus("archived problem cannot be judged")
	}
	if problem.ActiveJudgeRevision == "" {
		return Problem{}, ErrorInvalidStatus("problem has no published judge revision")
	}
	items, err := uc.testcases.ListTestcases(ctx, problemID, false)
	if err != nil {
		return Problem{}, err
	}
	if len(items) == 0 {
		return Problem{}, ErrorInvalidStatus("problem has no active testcases")
	}
	return problem, nil
}

type CreateProblemInput struct {
	Context   *commonv1.RequestContext
	Problem   Problem
	Tags      []string
	Testcases []TestcaseContent
}

type ProblemRepository interface {
	Create(context.Context, Problem, []string) (Problem, error)
	FindByID(context.Context, int64) (Problem, error)
	List(context.Context, int32, int32, bool) ([]Problem, int64, error)
	Update(context.Context, Problem, []string) (Problem, error)
	Archive(context.Context, int64) (Problem, error)
}

func (uc *ProblemUsecase) Archive(ctx context.Context, requestContext *commonv1.RequestContext, problemID int64) (Problem, error) {
	if uc == nil || uc.repo == nil {
		return Problem{}, ErrorInternal("problem repository is not configured")
	}
	if err := requireAdmin(requestContext); err != nil {
		return Problem{}, err
	}
	problem, err := uc.repo.FindByID(ctx, problemID)
	if err != nil {
		return Problem{}, err
	}
	if problem.Status == problemv1.ProblemStatus_PROBLEM_STATUS_ARCHIVED {
		return problem, nil
	}
	archived, err := uc.repo.Archive(ctx, problemID)
	if err == nil && uc.cache != nil {
		_ = uc.cache.Delete(ctx, problemID)
	}
	return archived, err
}

func (uc *ProblemUsecase) Update(ctx context.Context, requestContext *commonv1.RequestContext, problemID int64, input Problem, tags []string) (Problem, error) {
	if uc == nil || uc.repo == nil {
		return Problem{}, ErrorInternal("problem repository is not configured")
	}
	if err := requireAdmin(requestContext); err != nil {
		return Problem{}, err
	}
	current, err := uc.repo.FindByID(ctx, problemID)
	if err != nil {
		return Problem{}, err
	}
	if current.Status == problemv1.ProblemStatus_PROBLEM_STATUS_ARCHIVED {
		return Problem{}, ErrorInvalidStatus("archived problem cannot be updated")
	}
	input.ID = problemID
	input.Title = strings.TrimSpace(input.Title)
	input.Slug = strings.TrimSpace(input.Slug)
	input.Description = strings.TrimSpace(input.Description)
	input.Status = current.Status
	input.CreatedBy = current.CreatedBy
	input.CreatedAt = current.CreatedAt
	updated, err := uc.repo.Update(ctx, input, normalizeTags(tags))
	if err == nil && uc.cache != nil {
		_ = uc.cache.Delete(ctx, problemID)
	}
	return updated, err
}

type ProblemPage struct {
	Items    []Problem
	Page     int32
	PageSize int32
	Total    int64
}

func (uc *ProblemUsecase) List(ctx context.Context, requestContext *commonv1.RequestContext, page, pageSize int32) (ProblemPage, error) {
	if uc == nil || uc.repo == nil {
		return ProblemPage{}, ErrorInternal("problem repository is not configured")
	}
	if requestContext == nil || requestContext.GetUserId() <= 0 {
		return ProblemPage{}, ErrorInvalidArgument("invalid list problems request")
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	includeArchived := requireAdmin(requestContext) == nil
	items, total, err := uc.repo.List(ctx, page, pageSize, includeArchived)
	if err != nil {
		return ProblemPage{}, err
	}
	return ProblemPage{Items: items, Page: page, PageSize: pageSize, Total: total}, nil
}

func (uc *ProblemUsecase) Get(ctx context.Context, requestContext *commonv1.RequestContext, problemID int64) (Problem, error) {
	if uc == nil || uc.repo == nil {
		return Problem{}, ErrorInternal("problem repository is not configured")
	}
	if requestContext == nil || requestContext.GetUserId() <= 0 || problemID <= 0 {
		return Problem{}, ErrorInvalidArgument("invalid get problem request")
	}
	var problem Problem
	var err error
	if uc.cache != nil {
		var found bool
		problem, found, _ = uc.cache.Get(ctx, problemID)
		if !found {
			problem, err = uc.repo.FindByID(ctx, problemID)
			if err == nil {
				_ = uc.cache.Set(ctx, problem)
			}
		}
	} else {
		problem, err = uc.repo.FindByID(ctx, problemID)
	}
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
	repo            ProblemRepository
	testcases       TestcaseRepository
	testcaseCommits TestcaseChangeCommitter
	objects         ObjectStore
	compensator     ProblemCreationCompensator
	cache           ProblemCache
	problemLocks    sync.Map
}

func NewProblemUsecase(repo ProblemRepository) *ProblemUsecase {
	return &ProblemUsecase{repo: repo}
}

type ProblemCache interface {
	Get(context.Context, int64) (Problem, bool, error)
	Set(context.Context, Problem) error
	Delete(context.Context, int64) error
}

func NewProblemUsecaseWithDependencies(problems ProblemRepository, testcases TestcaseRepository, objects ObjectStore, compensator ProblemCreationCompensator, cache ProblemCache) *ProblemUsecase {
	testcaseCommits, _ := testcases.(TestcaseChangeCommitter)
	return &ProblemUsecase{repo: problems, testcases: testcases, testcaseCommits: testcaseCommits, objects: objects, compensator: compensator, cache: cache}
}

func (uc *ProblemUsecase) lockProblem(problemID int64) func() {
	value, _ := uc.problemLocks.LoadOrStore(problemID, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
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
	if len(input.Testcases) > 0 && (uc.testcases == nil || uc.objects == nil || uc.compensator == nil) {
		return Problem{}, ErrorInternal("testcase dependencies are not configured")
	}
	problem := input.Problem
	problem.Title = strings.TrimSpace(problem.Title)
	problem.Slug = strings.TrimSpace(problem.Slug)
	problem.Description = strings.TrimSpace(problem.Description)
	problem.Status = problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL
	problem.CreatedBy = input.Context.GetUserId()
	created, err := uc.repo.Create(ctx, problem, normalizeTags(input.Tags))
	if err == nil && uc.cache != nil {
		_ = uc.cache.Set(ctx, created)
	}
	if err != nil || len(input.Testcases) == 0 {
		return created, err
	}
	stored := make([]Testcase, 0, len(input.Testcases))
	for _, testcase := range input.Testcases {
		item, addErr := uc.addTestcaseToProblem(ctx, created, testcase.CaseNo, testcase.Input, testcase.Output)
		if addErr != nil {
			if uc.cache != nil {
				_ = uc.cache.Delete(ctx, created.ID)
			}
			for _, previous := range stored {
				_ = uc.objects.Delete(ctx, previous.InputObjectKey)
				_ = uc.objects.Delete(ctx, previous.OutputObjectKey)
			}
			if cleanupErr := uc.compensator.DeleteCreatedProblem(ctx, created.ID); cleanupErr != nil {
				return Problem{}, ErrorInternal("create problem cleanup failed: %v", cleanupErr)
			}
			return Problem{}, addErr
		}
		stored = append(stored, item)
		created, err = uc.repo.FindByID(ctx, created.ID)
		if err != nil {
			return Problem{}, err
		}
	}
	return created, nil
}

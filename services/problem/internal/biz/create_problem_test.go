package biz

import (
	"context"
	"errors"
	"testing"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
)

func TestCreateProblem(t *testing.T) {
	repo := &fakeProblemRepository{created: Problem{ID: 42}}
	uc := NewProblemUsecase(repo)
	input := validCreateInput()
	input.Problem.Title = "  Two Sum  "
	input.Tags = []string{" Array ", "array", "Hash"}

	_, err := uc.Create(context.Background(), input)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if repo.input.Title != "Two Sum" || repo.input.Status != problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL || repo.input.CreatedBy != 7 {
		t.Fatalf("repository problem = %+v", repo.input)
	}
	if len(repo.tags) != 2 || repo.tags[0] != "array" || repo.tags[1] != "hash" {
		t.Fatalf("repository tags = %v", repo.tags)
	}
}

func TestCreateProblemRejectsNonAdmin(t *testing.T) {
	input := validCreateInput()
	input.Context.Roles = []string{"user"}
	_, err := NewProblemUsecase(&fakeProblemRepository{}).Create(context.Background(), input)
	if !problemv1.IsProblemErrorReasonPermissionDenied(err) {
		t.Fatalf("expected permission denied, got %v", err)
	}
}

func TestCreateProblemRejectsTestcasesUntilStorageIsAvailable(t *testing.T) {
	input := validCreateInput()
	input.HasTestcases = true
	_, err := NewProblemUsecase(&fakeProblemRepository{}).Create(context.Background(), input)
	if !problemv1.IsProblemErrorReasonInvalidStatus(err) {
		t.Fatalf("expected invalid status, got %v", err)
	}
}

func TestCreateProblemPropagatesRepositoryError(t *testing.T) {
	want := errors.New("database unavailable")
	_, err := NewProblemUsecase(&fakeProblemRepository{err: want}).Create(context.Background(), validCreateInput())
	if !errors.Is(err, want) {
		t.Fatalf("Create() error = %v, want %v", err, want)
	}
}

func validCreateInput() CreateProblemInput {
	return CreateProblemInput{
		Context: &commonv1.RequestContext{UserId: 7, Roles: []string{"admin"}},
		Problem: Problem{Title: "Two Sum", Slug: "two-sum", Description: "Find two values.",
			Difficulty: problemv1.ProblemDifficulty_PROBLEM_DIFFICULTY_EASY, TimeLimitMs: 1000, MemoryLimitKb: 65536},
	}
}

type fakeProblemRepository struct {
	input   Problem
	tags    []string
	created Problem
	err     error
}

func (r *fakeProblemRepository) Create(_ context.Context, problem Problem, tags []string) (Problem, error) {
	r.input = problem
	r.tags = append([]string(nil), tags...)
	if r.err != nil {
		return Problem{}, r.err
	}
	if r.created.ID == 0 {
		r.created = problem
	}
	return r.created, nil
}

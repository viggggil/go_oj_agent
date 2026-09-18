package service

import (
	"context"
	"testing"

	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"github.com/viggggil/go_oj_agent/pkg/internalauth"
	"github.com/viggggil/go_oj_agent/services/problem/internal/biz"
)

func TestCreateProblemHandler(t *testing.T) {
	repo := &serviceFakeRepository{}
	server := NewProblemService(biz.NewProblemUsecase(repo))
	response, err := server.CreateProblem(internalauth.WithPrincipal(context.Background(), internalauth.Principal{ActorID: 9, ActorRoles: []string{"admin"}}), &problemv1.CreateProblemRequest{
		Problem: &problemv1.ProblemInput{Title: "Two Sum", Slug: "two-sum", Description: "Statement",
			Difficulty: problemv1.ProblemDifficulty_PROBLEM_DIFFICULTY_EASY, TimeLimitMs: 1000, MemoryLimitKb: 65536},
	})
	if err != nil {
		t.Fatalf("CreateProblem() error = %v", err)
	}
	if response.GetProblem().GetId() != 101 || response.GetProblem().GetCreatedBy() != 9 || response.GetProblem().GetStatus() != problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL {
		t.Fatalf("CreateProblem() response = %+v", response.GetProblem())
	}
}

func TestCreateProblemHandlerValidatesRequest(t *testing.T) {
	server := NewProblemService(biz.NewProblemUsecase(&serviceFakeRepository{}))
	_, err := server.CreateProblem(context.Background(), &problemv1.CreateProblemRequest{})
	if !problemv1.IsProblemErrorReasonInvalidArgument(err) {
		t.Fatalf("expected invalid argument, got %v", err)
	}
}

func TestCreateProblemHandlerForwardsTestcases(t *testing.T) {
	repo := &serviceTestcaseRepo{serviceFakeRepository: serviceFakeRepository{}}
	objects := &serviceObjectStore{}
	server := NewProblemService(biz.NewProblemUsecaseWithStore(repo, repo, objects, repo))
	response, err := server.CreateProblem(adminContext(), &problemv1.CreateProblemRequest{
		Problem:   &problemv1.ProblemInput{Title: "A+B", Slug: "a-plus-b", Description: "Statement", Difficulty: problemv1.ProblemDifficulty_PROBLEM_DIFFICULTY_EASY, TimeLimitMs: 1000, MemoryLimitKb: 65536},
		Testcases: []*problemv1.TestcaseInput{{CaseNo: 1, InputFilename: "1.in", InputContent: []byte("in"), OutputFilename: "1.out", OutputContent: []byte("out")}},
	})
	if err != nil || response.GetProblem().GetId() != 101 {
		t.Fatalf("response=%+v err=%v", response, err)
	}
}

type serviceFakeRepository struct {
	found biz.Problem
	err   error
}

func (r *serviceFakeRepository) Create(_ context.Context, problem biz.Problem, _ []string) (biz.Problem, error) {
	problem.ID = 101
	r.found = problem
	return problem, nil
}

func (r *serviceFakeRepository) FindByID(context.Context, int64) (biz.Problem, error) {
	return r.found, r.err
}

func (r *serviceFakeRepository) List(context.Context, int32, int32, bool) ([]biz.Problem, int64, error) {
	return nil, 0, r.err
}
func (r *serviceFakeRepository) Update(_ context.Context, problem biz.Problem, _ []string) (biz.Problem, error) {
	return problem, r.err
}
func (r *serviceFakeRepository) Archive(context.Context, int64) (biz.Problem, error) {
	r.found.Status = problemv1.ProblemStatus_PROBLEM_STATUS_ARCHIVED
	return r.found, r.err
}
func (r *serviceFakeRepository) DeleteCreatedProblem(context.Context, int64) error { return r.err }

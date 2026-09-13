package service

import (
	"context"
	"testing"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"github.com/viggggil/go_oj_agent/services/problem/internal/biz"
)

func TestCreateProblemHandler(t *testing.T) {
	repo := &serviceFakeRepository{}
	server := NewProblemService(biz.NewProblemUsecase(repo))
	response, err := server.CreateProblem(context.Background(), &problemv1.CreateProblemRequest{
		Context: &commonv1.RequestContext{UserId: 9, Roles: []string{"admin"}},
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

type serviceFakeRepository struct {
	found biz.Problem
	err   error
}

func (*serviceFakeRepository) Create(_ context.Context, problem biz.Problem, _ []string) (biz.Problem, error) {
	problem.ID = 101
	return problem, nil
}

func (r *serviceFakeRepository) FindByID(context.Context, int64) (biz.Problem, error) {
	return r.found, r.err
}

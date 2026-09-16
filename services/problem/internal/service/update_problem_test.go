package service

import (
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"github.com/viggggil/go_oj_agent/services/problem/internal/biz"
	"testing"
)

func TestUpdateProblemHandler(t *testing.T) {
	repo := &serviceFakeRepository{found: biz.Problem{ID: 3, Status: problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL, CreatedBy: 1}}
	response, err := NewProblemService(biz.NewProblemUsecase(repo)).UpdateProblem(adminContext(), &problemv1.UpdateProblemRequest{ProblemId: 3, Problem: &problemv1.ProblemInput{Title: "New", Slug: "new", Description: "Statement", Difficulty: problemv1.ProblemDifficulty_PROBLEM_DIFFICULTY_MEDIUM, TimeLimitMs: 1000, MemoryLimitKb: 65536}})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetProblem().GetTitle() != "New" {
		t.Fatalf("response = %+v", response)
	}
}

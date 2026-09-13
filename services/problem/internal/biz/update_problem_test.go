package biz

import (
	"context"
	"testing"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
)

func TestUpdateProblem(t *testing.T) {
	repo := &fakeProblemRepository{created: Problem{ID: 4, Status: problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL, CreatedBy: 3}}
	updated, err := NewProblemUsecase(repo).Update(context.Background(), &commonv1.RequestContext{UserId: 1, Roles: []string{"admin"}}, 4,
		Problem{Title: " New ", Slug: "new", Description: " Text ", Difficulty: problemv1.ProblemDifficulty_PROBLEM_DIFFICULTY_MEDIUM, TimeLimitMs: 2000, MemoryLimitKb: 65536}, []string{" DP "})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != "New" || updated.CreatedBy != 3 || len(repo.tags) != 1 || repo.tags[0] != "dp" {
		t.Fatalf("Update() = %+v, tags %v", updated, repo.tags)
	}
}

func TestUpdateProblemRejectsArchived(t *testing.T) {
	repo := &fakeProblemRepository{created: Problem{ID: 4, Status: problemv1.ProblemStatus_PROBLEM_STATUS_ARCHIVED}}
	_, err := NewProblemUsecase(repo).Update(context.Background(), &commonv1.RequestContext{UserId: 1, Roles: []string{"admin"}}, 4, Problem{}, nil)
	if !problemv1.IsProblemErrorReasonInvalidStatus(err) {
		t.Fatalf("expected invalid status, got %v", err)
	}
}

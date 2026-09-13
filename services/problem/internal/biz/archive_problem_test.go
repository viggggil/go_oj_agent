package biz

import (
	"context"
	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"testing"
)

func TestArchiveProblem(t *testing.T) {
	repo := &fakeProblemRepository{created: Problem{ID: 3, Status: problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL}}
	got, err := NewProblemUsecase(repo).Archive(context.Background(), &commonv1.RequestContext{UserId: 1, Roles: []string{"admin"}}, 3)
	if err != nil || got.Status != problemv1.ProblemStatus_PROBLEM_STATUS_ARCHIVED || !repo.archived {
		t.Fatalf("Archive() = %+v, %v", got, err)
	}
}
func TestArchiveProblemIsIdempotent(t *testing.T) {
	repo := &fakeProblemRepository{created: Problem{ID: 3, Status: problemv1.ProblemStatus_PROBLEM_STATUS_ARCHIVED}}
	_, err := NewProblemUsecase(repo).Archive(context.Background(), &commonv1.RequestContext{UserId: 1, Roles: []string{"admin"}}, 3)
	if err != nil || repo.archived {
		t.Fatalf("err=%v archived=%v", err, repo.archived)
	}
}

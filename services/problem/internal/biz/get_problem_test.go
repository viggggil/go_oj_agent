package biz

import (
	"context"
	"testing"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
)

func TestGetProblemVisibility(t *testing.T) {
	user := &commonv1.RequestContext{UserId: 1, Roles: []string{"user"}}
	admin := &commonv1.RequestContext{UserId: 2, Roles: []string{"admin"}}

	for _, status := range []problemv1.ProblemStatus{problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL, problemv1.ProblemStatus_PROBLEM_STATUS_ARCHIVED} {
		repo := &fakeProblemRepository{created: Problem{ID: 10, Status: status}}
		_, userErr := NewProblemUsecase(repo).Get(context.Background(), user, 10)
		if status == problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL && userErr != nil {
			t.Fatalf("normal problem rejected: %v", userErr)
		}
		if status == problemv1.ProblemStatus_PROBLEM_STATUS_ARCHIVED && !problemv1.IsProblemErrorReasonNotFound(userErr) {
			t.Fatalf("expected archived problem hidden, got %v", userErr)
		}
		if _, err := NewProblemUsecase(repo).Get(context.Background(), admin, 10); err != nil {
			t.Fatalf("admin get status %s: %v", status, err)
		}
	}
}

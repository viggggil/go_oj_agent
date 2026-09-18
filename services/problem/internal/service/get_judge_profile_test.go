package service

import (
	"context"
	"testing"

	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"github.com/viggggil/go_oj_agent/pkg/internalauth"
	"github.com/viggggil/go_oj_agent/services/problem/internal/biz"
)

func TestGetJudgeProfileHandler(t *testing.T) {
	repo := &serviceTestcaseRepo{
		serviceFakeRepository: serviceFakeRepository{found: biz.Problem{ID: 2, Status: problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL, TimeLimitMs: 1000, MemoryLimitKb: 65536, ActiveJudgeRevision: "01K5C6Y7N8P9Q0R1S2T3V4W5X6"}},
		items:                 []biz.Testcase{{ID: 1, ProblemID: 2, Status: problemv1.TestcaseStatus_TESTCASE_STATUS_ACTIVE}},
	}
	ctx := internalauth.WithPrincipal(context.Background(), internalauth.Principal{Caller: "judge-service"})
	response, err := NewProblemService(biz.NewProblemUsecaseWithStore(repo, repo, &serviceObjectStore{}, repo)).GetJudgeProfile(ctx, &problemv1.GetJudgeProfileRequest{ProblemId: 2})
	if err != nil || response.GetProfile().GetActiveJudgeRevision() != "01K5C6Y7N8P9Q0R1S2T3V4W5X6" {
		t.Fatalf("response=%+v err=%v", response, err)
	}
}

func TestGetJudgeProfileRejectsUntrustedCaller(t *testing.T) {
	repo := &serviceTestcaseRepo{}
	server := NewProblemService(biz.NewProblemUsecaseWithStore(repo, repo, &serviceObjectStore{}, repo))
	_, err := server.GetJudgeProfile(context.Background(), &problemv1.GetJudgeProfileRequest{ProblemId: 2})
	if err == nil {
		t.Fatal("expected missing principal to be rejected")
	}
	ctx := internalauth.WithPrincipal(context.Background(), internalauth.Principal{Caller: "gateway-service", ActorID: 1, ActorRoles: []string{"admin"}})
	_, err = server.GetJudgeProfile(ctx, &problemv1.GetJudgeProfileRequest{ProblemId: 2})
	if !problemv1.IsProblemErrorReasonPermissionDenied(err) {
		t.Fatalf("expected permission denied, got %v", err)
	}
}

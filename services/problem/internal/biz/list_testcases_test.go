package biz

import (
	"context"
	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	"testing"
)

func TestListTestcasesAllowsAdminAndJudge(t *testing.T) {
	for _, role := range []string{"admin", "judge"} {
		problems := &fakeProblemRepository{created: Problem{ID: 2}}
		repo := &fakeTestcaseRepository{created: Testcase{ID: 1}}
		items, err := NewProblemUsecaseWithStore(problems, repo, &fakeObjectStore{}, problems).ListTestcases(context.Background(), &commonv1.RequestContext{UserId: 1, Roles: []string{role}}, 2, false)
		if err != nil || len(items) != 1 {
			t.Fatalf("role=%s items=%v err=%v", role, items, err)
		}
	}
}
func TestListTestcasesRejectsUser(t *testing.T) {
	problems := &fakeProblemRepository{}
	_, err := NewProblemUsecaseWithStore(problems, &fakeTestcaseRepository{}, &fakeObjectStore{}, problems).ListTestcases(context.Background(), &commonv1.RequestContext{UserId: 1, Roles: []string{"user"}}, 2, false)
	if err == nil {
		t.Fatal("expected permission error")
	}
}

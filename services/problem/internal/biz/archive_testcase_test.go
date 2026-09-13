package biz

import (
	"context"
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"testing"
)

func TestArchiveTestcase(t *testing.T) {
	problems := &fakeProblemRepository{created: Problem{ID: 2}}
	repo := &fakeTestcaseRepository{}
	got, err := NewProblemUsecaseWithStore(problems, repo, &fakeObjectStore{}, problems).ArchiveTestcase(context.Background(), adminContext(), 2, 7)
	if err != nil || got.Status != problemv1.TestcaseStatus_TESTCASE_STATUS_ARCHIVED {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

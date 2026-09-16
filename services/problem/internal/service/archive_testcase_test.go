package service

import (
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"github.com/viggggil/go_oj_agent/services/problem/internal/biz"
	"testing"
)

func TestArchiveTestcaseHandler(t *testing.T) {
	repo := &serviceTestcaseRepo{serviceFakeRepository: serviceFakeRepository{found: biz.Problem{ID: 2}}}
	response, err := NewProblemService(biz.NewProblemUsecaseWithStore(repo, repo, &serviceObjectStore{}, repo)).ArchiveTestcase(adminContext(), &problemv1.ArchiveTestcaseRequest{ProblemId: 2, TestcaseId: 7})
	if err != nil || response.GetTestcase().GetStatus() != problemv1.TestcaseStatus_TESTCASE_STATUS_ARCHIVED {
		t.Fatalf("response=%+v err=%v", response, err)
	}
}

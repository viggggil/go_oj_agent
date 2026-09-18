package service

import (
	"crypto/sha256"
	"fmt"
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"github.com/viggggil/go_oj_agent/services/problem/internal/biz"
	"testing"
)

func TestArchiveTestcaseHandler(t *testing.T) {
	content := []byte("case")
	hash := sha256.Sum256(content)
	repo := &serviceTestcaseRepo{
		serviceFakeRepository: serviceFakeRepository{found: biz.Problem{ID: 2, Status: problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL, ActiveJudgeRevision: "01K5C6Y7N8P9Q0R1S2T3V4W5X6"}},
		items: []biz.Testcase{
			{ID: 7, ProblemID: 2, CaseNo: 1, Status: problemv1.TestcaseStatus_TESTCASE_STATUS_ACTIVE},
			{ID: 8, ProblemID: 2, CaseNo: 2, InputObjectKey: "in", OutputObjectKey: "out", InputSHA256: fmt.Sprintf("%x", hash), OutputSHA256: fmt.Sprintf("%x", hash), InputSizeBytes: int64(len(content)), OutputSizeBytes: int64(len(content)), Status: problemv1.TestcaseStatus_TESTCASE_STATUS_ACTIVE},
		},
	}
	objects := &serviceObjectStore{objects: map[string][]byte{"in": content, "out": content}}
	response, err := NewProblemService(biz.NewProblemUsecaseWithStore(repo, repo, objects, repo)).ArchiveTestcase(adminContext(), &problemv1.ArchiveTestcaseRequest{ProblemId: 2, TestcaseId: 7})
	if err != nil || response.GetTestcase().GetStatus() != problemv1.TestcaseStatus_TESTCASE_STATUS_ARCHIVED {
		t.Fatalf("response=%+v err=%v", response, err)
	}
}

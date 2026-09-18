package biz

import (
	"context"
	"crypto/sha256"
	"fmt"
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"testing"
)

func TestArchiveTestcase(t *testing.T) {
	problems := &fakeProblemRepository{created: Problem{ID: 2, Status: problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL, ActiveJudgeRevision: "01K5C6Y7N8P9Q0R1S2T3V4W5X6"}}
	content := []byte("case")
	hash := sha256.Sum256(content)
	repo := &fakeTestcaseRepository{items: []Testcase{
		{ID: 7, ProblemID: 2, CaseNo: 1, Status: problemv1.TestcaseStatus_TESTCASE_STATUS_ACTIVE},
		{ID: 8, ProblemID: 2, CaseNo: 2, InputObjectKey: "in", OutputObjectKey: "out", InputSHA256: fmt.Sprintf("%x", hash), OutputSHA256: fmt.Sprintf("%x", hash), InputSizeBytes: int64(len(content)), OutputSizeBytes: int64(len(content)), Status: problemv1.TestcaseStatus_TESTCASE_STATUS_ACTIVE},
	}}
	objects := &fakeObjectStore{objects: map[string][]byte{"in": content, "out": content}}
	got, err := NewProblemUsecaseWithStore(problems, repo, objects, problems).ArchiveTestcase(context.Background(), adminContext(), 2, 7)
	if err != nil || got.Status != problemv1.TestcaseStatus_TESTCASE_STATUS_ARCHIVED {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

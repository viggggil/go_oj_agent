package biz

import (
	"context"
	"testing"

	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
)

func TestGetJudgeProfile(t *testing.T) {
	problem := Problem{ID: 2, Status: problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL, TimeLimitMs: 1000, MemoryLimitKb: 65536, ActiveJudgeRevision: "01K5C6Y7N8P9Q0R1S2T3V4W5X6"}
	testcases := &fakeTestcaseRepository{items: []Testcase{{ID: 1, ProblemID: 2, Status: problemv1.TestcaseStatus_TESTCASE_STATUS_ACTIVE}}}
	got, err := NewProblemUsecaseWithStore(&fakeProblemRepository{created: problem}, testcases, &fakeObjectStore{}, &fakeProblemRepository{}).GetJudgeProfile(context.Background(), 2)
	if err != nil || got.ActiveJudgeRevision != problem.ActiveJudgeRevision {
		t.Fatalf("GetJudgeProfile() = %+v, %v", got, err)
	}
}

func TestGetJudgeProfileRejectsUnavailableProblem(t *testing.T) {
	tests := []struct {
		name      string
		problem   Problem
		testcases []Testcase
	}{
		{name: "archived", problem: Problem{ID: 2, Status: problemv1.ProblemStatus_PROBLEM_STATUS_ARCHIVED, ActiveJudgeRevision: "01K5C6Y7N8P9Q0R1S2T3V4W5X6"}, testcases: []Testcase{{ID: 1}}},
		{name: "missing revision", problem: Problem{ID: 2, Status: problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL}, testcases: []Testcase{{ID: 1}}},
		{name: "empty testcases", problem: Problem{ID: 2, Status: problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL, ActiveJudgeRevision: "01K5C6Y7N8P9Q0R1S2T3V4W5X6"}, testcases: []Testcase{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewProblemUsecaseWithStore(&fakeProblemRepository{created: tt.problem}, &fakeTestcaseRepository{items: tt.testcases}, &fakeObjectStore{}, &fakeProblemRepository{}).GetJudgeProfile(context.Background(), 2)
			if !problemv1.IsProblemErrorReasonInvalidStatus(err) {
				t.Fatalf("expected invalid status, got %v", err)
			}
		})
	}
}

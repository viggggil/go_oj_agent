package problemv1

import (
	"bytes"
	"testing"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	"google.golang.org/protobuf/proto"
)

func TestAddTestcaseRequestValidate(t *testing.T) {
	valid := &AddTestcaseRequest{
		ProblemId:      10,
		CaseNo:         1,
		InputFilename:  "1.in",
		InputContent:   []byte("1 2\n"),
		OutputFilename: "1.out",
		OutputContent:  []byte("3\n"),
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*AddTestcaseRequest)
	}{
		{name: "missing problem", mutate: func(req *AddTestcaseRequest) { req.ProblemId = 0 }},
		{name: "invalid problem", mutate: func(req *AddTestcaseRequest) { req.ProblemId = 0 }},
		{name: "wrong input suffix", mutate: func(req *AddTestcaseRequest) { req.InputFilename = "001.txt" }},
		{name: "non-canonical input number", mutate: func(req *AddTestcaseRequest) { req.InputFilename = "001.in" }},
		{name: "path input filename", mutate: func(req *AddTestcaseRequest) { req.InputFilename = "dir/001.in" }},
		{name: "wrong output suffix", mutate: func(req *AddTestcaseRequest) { req.OutputFilename = "001.ans" }},
		{name: "empty input", mutate: func(req *AddTestcaseRequest) { req.InputContent = nil }},
		{name: "oversized output", mutate: func(req *AddTestcaseRequest) {
			req.OutputContent = bytes.Repeat([]byte{'x'}, 16*1024*1024+1)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clone := proto.Clone(valid).(*AddTestcaseRequest)
			tt.mutate(clone)
			if err := clone.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestProblemRequestsValidate(t *testing.T) {
	input := &ProblemInput{
		Title:         "Two Sum",
		Slug:          "two-sum",
		Description:   "Find two values.",
		Difficulty:    ProblemDifficulty_PROBLEM_DIFFICULTY_EASY,
		TimeLimitMs:   1000,
		MemoryLimitKb: 65536,
		Tags:          []string{"array"},
	}
	if err := (&CreateProblemRequest{Problem: input}).Validate(); err != nil {
		t.Fatalf("valid create request rejected: %v", err)
	}
	if err := (&ListProblemsRequest{
		Page: &commonv1.PageRequest{Page: 1, PageSize: 20},
	}).Validate(); err != nil {
		t.Fatalf("valid list request rejected: %v", err)
	}

	invalid := proto.Clone(input).(*ProblemInput)
	invalid.Difficulty = ProblemDifficulty_PROBLEM_DIFFICULTY_UNSPECIFIED
	if err := (&CreateProblemRequest{Problem: invalid}).Validate(); err == nil {
		t.Fatal("expected unspecified difficulty to fail validation")
	}
}

func TestJudgeProfileContractValidate(t *testing.T) {
	if err := (&GetJudgeProfileRequest{ProblemId: 1}).Validate(); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	if err := (&GetJudgeProfileRequest{}).Validate(); err == nil {
		t.Fatal("expected missing problem id to fail validation")
	}
	profile := &JudgeProfile{ProblemId: 1, Status: ProblemStatus_PROBLEM_STATUS_NORMAL, TimeLimitMs: 1000, MemoryLimitKb: 65536, ActiveJudgeRevision: "01K5C6Y7N8P9Q0R1S2T3V4W5X6"}
	if err := profile.Validate(); err != nil {
		t.Fatalf("valid profile rejected: %v", err)
	}
	profile.ActiveJudgeRevision = "mutable"
	if err := profile.Validate(); err == nil {
		t.Fatal("expected invalid revision to fail validation")
	}
}

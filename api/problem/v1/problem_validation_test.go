package problemv1

import (
	"bytes"
	"testing"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	"google.golang.org/protobuf/proto"
)

func TestAddTestcaseRequestValidate(t *testing.T) {
	valid := &AddTestcaseRequest{
		Context:        &commonv1.RequestContext{UserId: 1, Roles: []string{"admin"}},
		ProblemId:      10,
		Version:        1,
		CaseNo:         1,
		InputFilename:  "001.in",
		InputContent:   []byte("1 2\n"),
		OutputFilename: "001.out",
		OutputContent:  []byte("3\n"),
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*AddTestcaseRequest)
	}{
		{name: "missing context", mutate: func(req *AddTestcaseRequest) { req.Context = nil }},
		{name: "invalid problem", mutate: func(req *AddTestcaseRequest) { req.ProblemId = 0 }},
		{name: "wrong input suffix", mutate: func(req *AddTestcaseRequest) { req.InputFilename = "001.txt" }},
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
	context := &commonv1.RequestContext{UserId: 1, Roles: []string{"admin"}}

	if err := (&CreateProblemRequest{Context: context, Problem: input}).Validate(); err != nil {
		t.Fatalf("valid create request rejected: %v", err)
	}
	if err := (&ListProblemsRequest{
		Context: context,
		Page:    &commonv1.PageRequest{Page: 1, PageSize: 20},
	}).Validate(); err != nil {
		t.Fatalf("valid list request rejected: %v", err)
	}

	invalid := proto.Clone(input).(*ProblemInput)
	invalid.Difficulty = ProblemDifficulty_PROBLEM_DIFFICULTY_UNSPECIFIED
	if err := (&CreateProblemRequest{Context: context, Problem: invalid}).Validate(); err == nil {
		t.Fatal("expected unspecified difficulty to fail validation")
	}
}

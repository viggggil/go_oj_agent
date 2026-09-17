package submissionv1

import (
	"strings"
	"testing"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	"google.golang.org/protobuf/proto"
)

func TestCreateSubmissionRequestValidate(t *testing.T) {
	valid := &CreateSubmissionRequest{
		ProblemId:      1001,
		Language:       "cpp",
		SourceCode:     "int main() { return 0; }",
		IdempotencyKey: "550e8400-e29b-41d4-a716-446655440000",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*CreateSubmissionRequest)
	}{
		{name: "missing problem", mutate: func(req *CreateSubmissionRequest) { req.ProblemId = 0 }},
		{name: "invalid language", mutate: func(req *CreateSubmissionRequest) { req.Language = "C++ (GCC)" }},
		{name: "empty source", mutate: func(req *CreateSubmissionRequest) { req.SourceCode = "" }},
		{name: "oversized source", mutate: func(req *CreateSubmissionRequest) { req.SourceCode = strings.Repeat("x", 1024*1024+1) }},
		{name: "missing idempotency key", mutate: func(req *CreateSubmissionRequest) { req.IdempotencyKey = "" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := proto.Clone(valid).(*CreateSubmissionRequest)
			tt.mutate(request)
			if err := request.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestSubmissionQueryRequestsValidate(t *testing.T) {
	if err := (&ListSubmissionsRequest{
		Page: &commonv1.PageRequest{Page: 1, PageSize: 20},
	}).Validate(); err != nil {
		t.Fatalf("valid list request rejected: %v", err)
	}
	if err := (&ListSubmissionsRequest{}).Validate(); err == nil {
		t.Fatal("expected missing page to fail validation")
	}
	if err := (&GetSubmissionRequest{SubmissionId: 0}).Validate(); err == nil {
		t.Fatal("expected invalid submission ID to fail validation")
	}
	if err := (&RejudgeSubmissionRequest{SubmissionId: 0, IdempotencyKey: "550e8400-e29b-41d4-a716-446655440000"}).Validate(); err == nil {
		t.Fatal("expected invalid rejudge submission ID to fail validation")
	}
	if err := (&RejudgeSubmissionRequest{SubmissionId: 1}).Validate(); err == nil {
		t.Fatal("expected missing rejudge idempotency key to fail validation")
	}
}

func TestSubmissionServiceMethods(t *testing.T) {
	want := map[string]bool{
		"CreateSubmission":  true,
		"GetSubmission":     true,
		"ListSubmissions":   true,
		"GetJudgeResult":    true,
		"RejudgeSubmission": true,
	}
	for _, method := range SubmissionService_ServiceDesc.Methods {
		delete(want, method.MethodName)
		if method.MethodName == "ListRecentSubmissions" {
			t.Fatal("ListRecentSubmissions must not be registered")
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing service methods: %v", want)
	}
}

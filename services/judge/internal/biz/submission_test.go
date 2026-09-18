package biz

import (
	"strings"
	"testing"
	"time"
)

const testRevision = "01K5C6Y7N8P9Q0R1S2T3V4W5X6"

func TestListFilterNormalized(t *testing.T) {
	got := (ListFilter{Page: -1, PageSize: 1000, Language: " Go "}).Normalized()
	if got.Page != 1 || got.PageSize != MaxPageSize || got.Language != "go" {
		t.Fatalf("Normalized() = %+v", got)
	}
	got = (ListFilter{}).Normalized()
	if got.Page != 1 || got.PageSize != DefaultPageSize {
		t.Fatalf("default Normalized() = %+v", got)
	}
}

func TestValidateSubmissionForCreate(t *testing.T) {
	now := time.Now().UTC()
	valid := Submission{
		UserID: 1, ProblemID: 2, Language: "cpp", SourceObjectKey: "sources/id/source.cpp",
		SourceSHA256: strings.Repeat("a", 64), SourceSizeBytes: 10,
		JudgeRevision: testRevision, JudgeDeadlineAt: now.Add(time.Minute),
	}
	if err := ValidateSubmissionForCreate(valid); err != nil {
		t.Fatalf("ValidateSubmissionForCreate() error = %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*Submission)
	}{
		{"actor", func(s *Submission) { s.UserID = 0 }},
		{"language", func(s *Submission) { s.Language = "rust" }},
		{"key", func(s *Submission) { s.SourceObjectKey = "" }},
		{"hash", func(s *Submission) { s.SourceSHA256 = strings.Repeat("A", 64) }},
		{"size", func(s *Submission) { s.SourceSizeBytes = 0 }},
		{"revision", func(s *Submission) { s.JudgeRevision = strings.Repeat("x", 26) }},
		{"deadline", func(s *Submission) { s.JudgeDeadlineAt = time.Time{} }},
		{"retries", func(s *Submission) { s.RetryCount = MaxRetryCount + 1 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := valid
			test.mutate(&input)
			if err := ValidateSubmissionForCreate(input); err == nil {
				t.Fatal("ValidateSubmissionForCreate() error = nil")
			}
		})
	}
}

func TestValidateIdempotency(t *testing.T) {
	now := time.Now().UTC()
	request := IdempotencyRequest{
		ActorID: 1, Operation: OperationCreateSubmission,
		Key:         "123e4567-e89b-12d3-a456-426614174000",
		RequestHash: strings.Repeat("b", 64), ExpiresAt: now.Add(time.Hour),
	}
	if err := ValidateIdempotency(request, OperationCreateSubmission, now); err != nil {
		t.Fatalf("ValidateIdempotency() error = %v", err)
	}
	request.RequestHash = strings.Repeat("B", 64)
	if err := ValidateIdempotency(request, OperationCreateSubmission, now); err == nil {
		t.Fatal("uppercase request hash was accepted")
	}
}

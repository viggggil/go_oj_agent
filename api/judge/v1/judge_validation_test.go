package judgev1

import "testing"

func TestJudgeTaskValidate(t *testing.T) {
	task := &JudgeTask{
		EventId:         "550e8400-e29b-41d4-a716-446655440000",
		SubmissionId:    90001,
		ProblemId:       1001,
		Language:        "cpp",
		JudgeRevision:   "01K5C6Y7N8P9Q0R1S2T3V4W5X6",
		SourceObjectKey: "sources/01K5C6Y7N8P9Q0R1S2T3V4W5X6/source.cpp",
		SourceSha256:    "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		SourceSizeBytes: 24,
	}
	if err := task.Validate(); err != nil {
		t.Fatalf("valid task rejected: %v", err)
	}

	task.JudgeRevision = "mutable"
	if err := task.Validate(); err == nil {
		t.Fatal("expected invalid judge revision to fail validation")
	}
}

func TestJudgeFailureValidate(t *testing.T) {
	failure := &JudgeFailure{
		EventId:       "550e8400-e29b-41d4-a716-446655440000",
		SubmissionId:  90001,
		JudgeRevision: "01K5C6Y7N8P9Q0R1S2T3V4W5X6",
		Reason:        "SANDBOX_UNAVAILABLE",
		Retryable:     true,
	}
	if err := failure.Validate(); err != nil {
		t.Fatalf("valid failure rejected: %v", err)
	}
}

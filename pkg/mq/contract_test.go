package mq

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestJudgeTaskEnvelopeRoundTrip(t *testing.T) {
	now := time.Date(2026, 9, 23, 1, 2, 3, 0, time.UTC)
	task := JudgeTask{
		SubmissionID: 9, ProblemID: 7, Language: "go", JudgeRevision: "01K5C6Y7N8P9Q0R1S2T3V4W5X6",
		SourceObjectKey: "sources/id/source.go", SourceSHA256: strings.Repeat("a", 64), SourceSizeBytes: 13,
		JudgeDeadlineAt: now.Add(time.Minute),
	}
	if err := task.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	body, err := MarshalEnvelope(EnvelopeMetadata{
		EventID: "123e4567-e89b-12d3-a456-426614174000", EventType: EventTypeJudgeTask,
		EventVersion: EventVersion1, OccurredAt: now, TraceID: "trace-1",
	}, task)
	if err != nil {
		t.Fatalf("MarshalEnvelope() error = %v", err)
	}
	var decoded JudgeTask
	envelope, err := UnmarshalEnvelope(body, EventTypeJudgeTask, &decoded)
	if err != nil {
		t.Fatalf("UnmarshalEnvelope() error = %v", err)
	}
	if envelope.EventID == "" || decoded != task {
		t.Fatalf("envelope=%+v task=%+v", envelope, decoded)
	}
}

func TestJudgeResultEnvelopeRoundTrip(t *testing.T) {
	now := time.Date(2026, 9, 23, 1, 2, 3, 0, time.UTC)
	tests := []struct {
		name      string
		eventType string
		value     any
		decoded   any
	}{
		{
			name: "completed", eventType: EventTypeJudgeCompleted,
			value: JudgeCompleted{SubmissionID: 9, JudgeRevision: "01K5C6Y7N8P9Q0R1S2T3V4W5X6", Verdict: "AC", TimeMS: 3, MemoryKB: 4,
				CaseResults: []JudgeCaseResult{{CaseNo: 1, Verdict: "AC", TimeMS: 3, MemoryKB: 4}}},
			decoded: &JudgeCompleted{},
		},
		{
			name: "failed", eventType: EventTypeJudgeFailed,
			value:   JudgeFailed{SubmissionID: 9, JudgeRevision: "01K5C6Y7N8P9Q0R1S2T3V4W5X6", Reason: "SANDBOX_UNAVAILABLE", Retryable: true},
			decoded: &JudgeFailed{},
		},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body, err := MarshalEnvelope(EnvelopeMetadata{
				EventID:   fmt.Sprintf("123e4567-e89b-12d3-a456-42661417400%d", index),
				EventType: test.eventType, EventVersion: EventVersion1, OccurredAt: now,
			}, test.value)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = UnmarshalEnvelope(body, test.eventType, test.decoded); err != nil {
				t.Fatal(err)
			}
			switch value := test.decoded.(type) {
			case *JudgeCompleted:
				err = value.Validate()
			case *JudgeFailed:
				err = value.Validate()
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

package biz

import (
	"testing"
	"time"

	submissionv1 "github.com/viggggil/go_oj_agent/api/submission/v1"
	"github.com/viggggil/go_oj_agent/pkg/mq"
)

func TestParseJudgeCompletedEvent(t *testing.T) {
	body, _ := mq.MarshalEnvelope(mq.EnvelopeMetadata{
		EventID: "123e4567-e89b-12d3-a456-426614174000", EventType: EventTypeJudgeCompleted,
		EventVersion: mq.EventVersion1, OccurredAt: time.Now().UTC(),
	}, mq.JudgeCompleted{
		SubmissionID: 9, JudgeRevision: "01K5C6Y7N8P9Q0R1S2T3V4W5X6", Verdict: "AC", TimeMS: 3, MemoryKB: 4,
		CaseResults: []mq.JudgeCaseResult{{CaseNo: 1, Verdict: "AC", TimeMS: 3, MemoryKB: 4}},
	})
	event, err := ParseJudgeResultEvent(EventTypeJudgeCompleted, body)
	if err != nil || event.Verdict != submissionv1.JudgeVerdict_JUDGE_VERDICT_AC || len(event.CaseResults) != 1 {
		t.Fatalf("ParseJudgeResultEvent() = %+v, %v", event, err)
	}
}

func TestParseJudgeResultRejectsRevisionAndTypeMismatch(t *testing.T) {
	body, _ := mq.MarshalEnvelope(mq.EnvelopeMetadata{
		EventID: "123e4567-e89b-12d3-a456-426614174001", EventType: EventTypeJudgeFailed,
		EventVersion: mq.EventVersion1, OccurredAt: time.Now().UTC(),
	}, mq.JudgeFailed{SubmissionID: 9, JudgeRevision: "short", Reason: "x"})
	if _, err := ParseJudgeResultEvent(EventTypeJudgeCompleted, body); err == nil {
		t.Fatal("expected event type mismatch")
	}
}

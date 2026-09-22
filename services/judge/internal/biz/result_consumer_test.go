package biz

import (
	"encoding/json"
	"testing"

	submissionv1 "github.com/viggggil/go_oj_agent/api/submission/v1"
)

func TestParseJudgeCompletedEvent(t *testing.T) {
	body, _ := json.Marshal(map[string]any{"event_id": "evt-1", "event_type": EventTypeJudgeCompleted, "event_version": 1, "payload": map[string]any{
		"submission_id": 9, "judge_revision": "01K5C6Y7N8P9Q0R1S2T3V4W5X6", "verdict": "AC", "time_ms": 3, "memory_kb": 4,
		"case_results": []any{map[string]any{"case_no": 1, "verdict": "AC", "time_ms": 3, "memory_kb": 4}},
	}})
	event, err := ParseJudgeResultEvent(EventTypeJudgeCompleted, body)
	if err != nil || event.Verdict != submissionv1.JudgeVerdict_JUDGE_VERDICT_AC || len(event.CaseResults) != 1 {
		t.Fatalf("ParseJudgeResultEvent() = %+v, %v", event, err)
	}
}

func TestParseJudgeResultRejectsRevisionAndTypeMismatch(t *testing.T) {
	body := []byte(`{"event_id":"evt-1","event_type":"judge.failed","payload":{"submission_id":9,"judge_revision":"short","reason":"x"}}`)
	if _, err := ParseJudgeResultEvent(EventTypeJudgeCompleted, body); err == nil {
		t.Fatal("expected event type mismatch")
	}
}

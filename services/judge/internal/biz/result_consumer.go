package biz

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	submissionv1 "github.com/viggggil/go_oj_agent/api/submission/v1"
)

const (
	EventTypeJudgeCompleted   = "judge.completed"
	EventTypeJudgeFailed      = "judge.failed"
	EventTypeSubmissionJudged = "submission.judged"
)

type SubmissionJudgedPayload struct {
	SubmissionID int64     `json:"submission_id"`
	UserID       int64     `json:"user_id"`
	ProblemID    int64     `json:"problem_id"`
	Verdict      string    `json:"verdict"`
	JudgedAt     time.Time `json:"judged_at"`
}

type JudgeResultEvent struct {
	EventID       string
	EventType     string
	EventVersion  int32
	SubmissionID  int64
	JudgeRevision string
	Verdict       submissionv1.JudgeVerdict
	TimeMS        *int32
	MemoryKB      *int32
	CaseResults   []CaseResult
	Reason        string
	Retryable     bool
	OccurredAt    time.Time
}

type ResultConsumer struct{ repository JudgeResultRepository }

func NewResultConsumer(repository JudgeResultRepository) *ResultConsumer {
	return &ResultConsumer{repository: repository}
}

func (c *ResultConsumer) Handle(ctx context.Context, eventType string, body []byte) error {
	if c == nil || c.repository == nil {
		return ErrorInternal("judge result repository is not configured")
	}
	event, err := ParseJudgeResultEvent(eventType, body)
	if err != nil {
		return err
	}
	return c.repository.ApplyJudgeResult(ctx, event)
}

func ParseJudgeResultEvent(eventType string, body []byte) (JudgeResultEvent, error) {
	var envelope struct {
		EventID      string          `json:"event_id"`
		EventType    string          `json:"event_type"`
		EventVersion int32           `json:"event_version"`
		Payload      json.RawMessage `json:"payload"`
		Data         json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return JudgeResultEvent{}, ErrorInvalidArgument("invalid judge result envelope")
	}
	if envelope.EventType != "" && eventType != "" && envelope.EventType != eventType {
		return JudgeResultEvent{}, ErrorInvalidArgument("judge result event type mismatch")
	}
	if eventType == "" {
		eventType = envelope.EventType
	}
	payload := envelope.Payload
	if len(payload) == 0 {
		payload = envelope.Data
	}
	if envelope.EventID == "" || len(payload) == 0 {
		return JudgeResultEvent{}, ErrorInvalidArgument("judge result envelope is incomplete")
	}
	result := JudgeResultEvent{EventID: envelope.EventID, EventType: eventType, EventVersion: envelope.EventVersion}
	switch eventType {
	case EventTypeJudgeCompleted:
		var value struct {
			SubmissionID  int64  `json:"submission_id"`
			JudgeRevision string `json:"judge_revision"`
			Verdict       string `json:"verdict"`
			TimeMS        int32  `json:"time_ms"`
			MemoryKB      int32  `json:"memory_kb"`
			CaseResults   []struct {
				CaseNo   int32  `json:"case_no"`
				Verdict  string `json:"verdict"`
				TimeMS   int32  `json:"time_ms"`
				MemoryKB int32  `json:"memory_kb"`
				Message  string `json:"message"`
			} `json:"case_results"`
			OccurredAt time.Time `json:"occurred_at"`
		}
		if err := json.Unmarshal(payload, &value); err != nil {
			return JudgeResultEvent{}, ErrorInvalidArgument("invalid judge completed payload")
		}
		result.SubmissionID, result.JudgeRevision, result.OccurredAt = value.SubmissionID, value.JudgeRevision, value.OccurredAt
		if value.OccurredAt.IsZero() {
			result.OccurredAt = time.Now().UTC()
		}
		result.Verdict, _ = verdictFromWire(value.Verdict)
		if result.Verdict == submissionv1.JudgeVerdict_JUDGE_VERDICT_UNSPECIFIED || result.SubmissionID <= 0 || len(result.JudgeRevision) != 26 {
			return JudgeResultEvent{}, ErrorInvalidArgument("invalid judge completed payload")
		}
		result.TimeMS, result.MemoryKB = &value.TimeMS, &value.MemoryKB
		for _, item := range value.CaseResults {
			verdict, ok := verdictFromWire(item.Verdict)
			if !ok || item.CaseNo <= 0 {
				return JudgeResultEvent{}, ErrorInvalidArgument("invalid judge case result")
			}
			tm, mm := item.TimeMS, item.MemoryKB
			result.CaseResults = append(result.CaseResults, CaseResult{CaseNo: item.CaseNo, Verdict: verdict, TimeMS: &tm, MemoryKB: &mm, Message: item.Message})
		}
	case EventTypeJudgeFailed:
		var value struct {
			SubmissionID  int64     `json:"submission_id"`
			JudgeRevision string    `json:"judge_revision"`
			Reason        string    `json:"reason"`
			Retryable     bool      `json:"retryable"`
			OccurredAt    time.Time `json:"occurred_at"`
		}
		if err := json.Unmarshal(payload, &value); err != nil || value.SubmissionID <= 0 || len(value.JudgeRevision) != 26 || strings.TrimSpace(value.Reason) == "" {
			return JudgeResultEvent{}, ErrorInvalidArgument("invalid judge failed payload")
		}
		result.SubmissionID, result.JudgeRevision, result.Reason, result.Retryable, result.OccurredAt = value.SubmissionID, value.JudgeRevision, value.Reason, value.Retryable, value.OccurredAt
		if result.OccurredAt.IsZero() {
			result.OccurredAt = time.Now().UTC()
		}
	default:
		return JudgeResultEvent{}, ErrorInvalidArgument("unsupported judge result event %q", eventType)
	}
	return result, nil
}

func verdictFromWire(value string) (submissionv1.JudgeVerdict, bool) {
	value = strings.ToUpper(strings.TrimSpace(value))
	if strings.HasPrefix(value, "JUDGE_VERDICT_") {
		value = strings.TrimPrefix(value, "JUDGE_VERDICT_")
	}
	for _, item := range []submissionv1.JudgeVerdict{submissionv1.JudgeVerdict_JUDGE_VERDICT_AC, submissionv1.JudgeVerdict_JUDGE_VERDICT_WA, submissionv1.JudgeVerdict_JUDGE_VERDICT_TLE, submissionv1.JudgeVerdict_JUDGE_VERDICT_MLE, submissionv1.JudgeVerdict_JUDGE_VERDICT_RE, submissionv1.JudgeVerdict_JUDGE_VERDICT_CE} {
		if item.String() == "JUDGE_VERDICT_"+value {
			return item, true
		}
	}
	return submissionv1.JudgeVerdict_JUDGE_VERDICT_UNSPECIFIED, false
}

func (e JudgeResultEvent) Validate() error {
	if e.EventID == "" || e.SubmissionID <= 0 || len(e.JudgeRevision) != 26 {
		return fmt.Errorf("invalid judge result identity")
	}
	return nil
}

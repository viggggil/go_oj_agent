package biz

import (
	"context"
	"fmt"
	"strings"
	"time"

	submissionv1 "github.com/viggggil/go_oj_agent/api/submission/v1"
	"github.com/viggggil/go_oj_agent/pkg/mq"
)

const (
	EventTypeJudgeCompleted   = mq.EventTypeJudgeCompleted
	EventTypeJudgeFailed      = mq.EventTypeJudgeFailed
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
	switch eventType {
	case EventTypeJudgeCompleted:
		var value mq.JudgeCompleted
		envelope, err := mq.UnmarshalEnvelope(body, eventType, &value)
		if err != nil || value.Validate() != nil {
			return JudgeResultEvent{}, ErrorInvalidArgument("invalid judge completed payload")
		}
		result := JudgeResultEvent{EventID: envelope.EventID, EventType: eventType, EventVersion: envelope.EventVersion}
		result.SubmissionID, result.JudgeRevision, result.OccurredAt = value.SubmissionID, value.JudgeRevision, envelope.OccurredAt
		result.Verdict, _ = verdictFromWire(value.Verdict)
		result.TimeMS, result.MemoryKB = &value.TimeMS, &value.MemoryKB
		for _, item := range value.CaseResults {
			verdict, ok := verdictFromWire(item.Verdict)
			if !ok {
				return JudgeResultEvent{}, ErrorInvalidArgument("invalid judge case result")
			}
			tm, mm := item.TimeMS, item.MemoryKB
			result.CaseResults = append(result.CaseResults, CaseResult{CaseNo: item.CaseNo, Verdict: verdict, TimeMS: &tm, MemoryKB: &mm, Message: item.Message})
		}
		return result, nil
	case EventTypeJudgeFailed:
		var value mq.JudgeFailed
		envelope, err := mq.UnmarshalEnvelope(body, eventType, &value)
		if err != nil || value.Validate() != nil {
			return JudgeResultEvent{}, ErrorInvalidArgument("invalid judge failed payload")
		}
		return JudgeResultEvent{
			EventID: envelope.EventID, EventType: eventType, EventVersion: envelope.EventVersion,
			SubmissionID: value.SubmissionID, JudgeRevision: value.JudgeRevision,
			Reason: value.Reason, Retryable: value.Retryable, OccurredAt: envelope.OccurredAt,
		}, nil
	default:
		return JudgeResultEvent{}, ErrorInvalidArgument("unsupported judge result event %q", eventType)
	}
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

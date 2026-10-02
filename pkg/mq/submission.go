package mq

import (
	"fmt"
	"time"
)

const (
	EventSubmissionJudged        = "submission.judged"
	RoutingSubmissionJudged      = EventSubmissionJudged
	EventSubmissionInvalidated   = "submission.invalidated"
	RoutingSubmissionInvalidated = EventSubmissionInvalidated
)

type SubmissionJudged struct {
	SubmissionID int64     `json:"submission_id"`
	ContestID    int64     `json:"contest_id"`
	UserID       int64     `json:"user_id"`
	ProblemID    int64     `json:"problem_id"`
	Verdict      string    `json:"verdict"`
	SubmittedAt  time.Time `json:"submitted_at"`
	JudgedAt     time.Time `json:"judged_at"`
}

func (e SubmissionJudged) Validate() error {
	if e.SubmissionID <= 0 || e.ContestID <= 0 || e.UserID <= 0 || e.ProblemID <= 0 || e.SubmittedAt.IsZero() || e.JudgedAt.IsZero() {
		return fmt.Errorf("invalid submission judged identity or timestamps")
	}
	switch e.Verdict {
	case "AC", "WA", "TLE", "MLE", "RE", "CE", "SYSTEM_ERROR":
		return nil
	default:
		return fmt.Errorf("invalid submission judged verdict")
	}
}

// ContestID/SubmittedAt 为比赛投影补充字段；普通提交的作废事件仍允许 ContestID 为零。
type SubmissionInvalidated struct {
	SubmissionID    int64     `json:"submission_id"`
	ContestID       int64     `json:"contest_id,omitempty"`
	UserID          int64     `json:"user_id"`
	ProblemID       int64     `json:"problem_id"`
	PreviousVerdict string    `json:"previous_verdict,omitempty"`
	SubmittedAt     time.Time `json:"submitted_at,omitempty"`
	InvalidatedAt   time.Time `json:"invalidated_at"`
}

func (e SubmissionInvalidated) Validate() error {
	if e.SubmissionID <= 0 || e.ContestID < 0 || e.UserID <= 0 || e.ProblemID <= 0 || e.InvalidatedAt.IsZero() || (e.ContestID > 0 && e.SubmittedAt.IsZero()) {
		return fmt.Errorf("invalid submission invalidated event")
	}
	return nil
}

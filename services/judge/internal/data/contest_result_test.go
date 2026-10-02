package data

import (
	"context"
	"fmt"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	submissionv1 "github.com/viggggil/go_oj_agent/api/submission/v1"
	"github.com/viggggil/go_oj_agent/services/judge/internal/biz"
)

func TestContestResultOutboxAtomicity(t *testing.T) {
	for _, tt := range []struct {
		name                  string
		contest               any
		failed, outboxFailure bool
	}{
		{name: "ordinary result has no contest event"},
		{name: "contest outbox failure rolls back result", contest: int64(3), outboxFailure: true},
		{name: "final infrastructure failure emits SYSTEM_ERROR", contest: int64(3), failed: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store, mock, now := newMockStore(t)
			event := biz.JudgeResultEvent{EventID: "123e4567-e89b-12d3-a456-426614174010", EventType: biz.EventTypeJudgeCompleted, EventVersion: 1, SubmissionID: 44, JudgeRevision: dataTestRevision, Verdict: submissionv1.JudgeVerdict_JUDGE_VERDICT_AC, OccurredAt: now}
			if tt.failed {
				event.EventType = biz.EventTypeJudgeFailed
				event.Message = "sandbox unavailable"
			}
			mock.ExpectBegin()
			mock.ExpectExec("INSERT IGNORE INTO processed_events").WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectQuery("SELECT user_id, problem_id, judge_revision, status, contest_id, created_at").WithArgs(int64(44)).WillReturnRows(sqlmock.NewRows([]string{"user_id", "problem_id", "judge_revision", "status", "contest_id", "created_at"}).AddRow(5, 7, dataTestRevision, "RUNNING", tt.contest, now))
			update := mock.ExpectExec("UPDATE submissions SET status")
			verdict := "AC"
			if tt.failed {
				verdict = "SYSTEM_ERROR"
				update.WithArgs("DONE", "SYSTEM_ERROR", event.Message, now, now, int64(44))
			} else {
				update.WithArgs("DONE", "AC", nil, nil, now, now, int64(44))
			}
			update.WillReturnResult(sqlmock.NewResult(0, 1))
			if tt.contest != nil {
				insert := mock.ExpectExec("INSERT INTO outbox_events").WithArgs(sqlmock.AnyArg(), int64(44), biz.EventTypeSubmissionJudged, jsonArgument{biz.SubmissionJudgedPayload{SubmissionID: 44, ContestID: 3, UserID: 5, ProblemID: 7, Verdict: verdict, SubmittedAt: now, JudgedAt: now}}, now)
				if tt.outboxFailure {
					insert.WillReturnError(fmt.Errorf("outbox unavailable"))
				} else {
					insert.WillReturnResult(sqlmock.NewResult(1, 1))
				}
			}
			if tt.outboxFailure {
				mock.ExpectRollback()
			} else {
				mock.ExpectCommit()
			}
			err := store.ApplyJudgeResult(context.Background(), event)
			if (err != nil) != tt.outboxFailure {
				t.Fatalf("err=%v", err)
			}
			assertExpectations(t, mock)
		})
	}
}

func TestContestRejudgePreservesSubmissionTime(t *testing.T) {
	store, mock, now := newMockStore(t)
	command := rejudgeCommand(now)
	old := storedSubmission(now.Add(-2*biz.JudgeQueueDeadline), submissionv1.SubmissionStatus_SUBMISSION_STATUS_DONE)
	old.ContestID = 3
	mock.ExpectBegin()
	expectIdempotencyReservation(mock, command.Idempotency, now, 1)
	mock.ExpectQuery("SELECT .* FROM submissions WHERE id = .* FOR UPDATE").WillReturnRows(submissionRow(old))
	mock.ExpectExec("UPDATE submissions").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO outbox_events").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO submissions").WillReturnResult(sqlmock.NewResult(45, 1))
	mock.ExpectExec("INSERT INTO outbox_events").WillReturnResult(sqlmock.NewResult(2, 1))
	mock.ExpectExec("UPDATE idempotency_requests").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	result, err := store.InvalidateAndRequeueWithOutboxAndIdempotency(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Submission.CreatedAt.Equal(old.CreatedAt) || !result.Submission.UpdatedAt.Equal(now) || result.Submission.ContestID != 3 {
		t.Fatalf("replacement=%+v", result.Submission)
	}
	assertExpectations(t, mock)
}

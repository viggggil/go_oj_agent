package data

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	mysql "github.com/go-sql-driver/mysql"

	submissionv1 "github.com/viggggil/go_oj_agent/api/submission/v1"
	"github.com/viggggil/go_oj_agent/services/judge/internal/biz"
)

const (
	createKey        = "123e4567-e89b-12d3-a456-426614174000"
	rejudgeKey       = "123e4567-e89b-12d3-a456-426614174001"
	requestedEvent   = "123e4567-e89b-12d3-a456-426614174002"
	invalidatedEvent = "123e4567-e89b-12d3-a456-426614174003"
	secondRequested  = "123e4567-e89b-12d3-a456-426614174004"
)

type jsonArgument struct{ expected any }

func (m jsonArgument) Match(value driver.Value) bool {
	var actual any
	var expected any
	bytes, ok := value.([]byte)
	if !ok {
		if text, stringOK := value.(string); stringOK {
			bytes = []byte(text)
		} else {
			return false
		}
	}
	want, err := json.Marshal(m.expected)
	if err != nil || json.Unmarshal(bytes, &actual) != nil || json.Unmarshal(want, &expected) != nil {
		return false
	}
	return actualJSONEqual(actual, expected)
}

func actualJSONEqual(left, right any) bool {
	l, _ := json.Marshal(left)
	r, _ := json.Marshal(right)
	return string(l) == string(r)
}

func TestStoreCreateWithOutboxAndIdempotencyCommitsAtomically(t *testing.T) {
	store, mock, now := newMockStore(t)
	command := createCommand(now)
	mock.ExpectBegin()
	expectIdempotencyReservation(mock, command.Idempotency, now, 1)
	mock.ExpectExec("INSERT INTO submissions").
		WithArgs(int64(5), int64(7), "go", "sources/id/source.go", strings.Repeat("a", 64), int64(13), dataTestRevision, "QUEUED", nil, nil, nil, int32(0), nil, now.Add(time.Minute), now, nil, nil, now).
		WillReturnResult(sqlmock.NewResult(101, 1))
	mock.ExpectExec("INSERT INTO outbox_events").
		WithArgs(requestedEvent, int64(101), biz.EventTypeJudgeRequested, jsonArgument{biz.JudgeRequestedPayload{
			SubmissionID: 101, ProblemID: 7, Language: "go", JudgeRevision: dataTestRevision,
			SourceObjectKey: "sources/id/source.go", SourceSHA256: strings.Repeat("a", 64), SourceSizeBytes: 13,
		}}, now).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("UPDATE idempotency_requests").
		WithArgs(jsonArgument{biz.CreateSubmissionResult{SubmissionID: 101, Status: submissionv1.SubmissionStatus_SUBMISSION_STATUS_QUEUED}}, int64(5), biz.OperationCreateSubmission, createKey).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	result, err := store.CreateWithOutboxAndIdempotency(context.Background(), command)
	if err != nil {
		t.Fatalf("CreateWithOutboxAndIdempotency() error = %v", err)
	}
	if result.SubmissionID != 101 || result.Status != submissionv1.SubmissionStatus_SUBMISSION_STATUS_QUEUED || result.Replayed {
		t.Fatalf("result = %+v", result)
	}
	assertExpectations(t, mock)
}

func TestStoreCreateRollsBackWhenOutboxInsertFails(t *testing.T) {
	store, mock, now := newMockStore(t)
	command := createCommand(now)
	mock.ExpectBegin()
	expectIdempotencyReservation(mock, command.Idempotency, now, 1)
	mock.ExpectExec("INSERT INTO submissions").WillReturnResult(sqlmock.NewResult(101, 1))
	mock.ExpectExec("INSERT INTO outbox_events").WillReturnError(errors.New("outbox failed"))
	mock.ExpectRollback()

	_, err := store.CreateWithOutboxAndIdempotency(context.Background(), command)
	if !biz.HasReason(err, biz.ReasonInternal) {
		t.Fatalf("error = %v, want internal", err)
	}
	assertExpectations(t, mock)
}

func TestStoreCreateIdempotencyBranches(t *testing.T) {
	t.Run("replay", func(t *testing.T) {
		store, mock, now := newMockStore(t)
		command := createCommand(now)
		mock.ExpectBegin()
		expectIdempotencyReservation(mock, command.Idempotency, now, 0)
		mock.ExpectQuery("SELECT request_hash, response").
			WithArgs(int64(5), biz.OperationCreateSubmission, createKey).
			WillReturnRows(sqlmock.NewRows([]string{"request_hash", "response"}).AddRow(command.Idempotency.RequestHash, []byte(`{"submission_id":101,"status":1}`)))
		mock.ExpectCommit()
		result, err := store.CreateWithOutboxAndIdempotency(context.Background(), command)
		if err != nil || result.SubmissionID != 101 || !result.Replayed {
			t.Fatalf("result = %+v, error = %v", result, err)
		}
		assertExpectations(t, mock)
	})

	tests := []struct {
		name       string
		storedHash string
		response   any
		reason     string
	}{
		{"different request", strings.Repeat("b", 64), nil, biz.ReasonIdempotencyConflict},
		{"in progress", strings.Repeat("a", 64), nil, biz.ReasonIdempotencyInProgress},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, mock, now := newMockStore(t)
			command := createCommand(now)
			mock.ExpectBegin()
			expectIdempotencyReservation(mock, command.Idempotency, now, 0)
			mock.ExpectQuery("SELECT request_hash, response").
				WithArgs(int64(5), biz.OperationCreateSubmission, createKey).
				WillReturnRows(sqlmock.NewRows([]string{"request_hash", "response"}).AddRow(test.storedHash, test.response))
			mock.ExpectRollback()
			_, err := store.CreateWithOutboxAndIdempotency(context.Background(), command)
			if !biz.HasReason(err, test.reason) {
				t.Fatalf("error = %v, want %s", err, test.reason)
			}
			assertExpectations(t, mock)
		})
	}
}

func TestStoreInvalidateAndRequeueCommitsAtomically(t *testing.T) {
	store, mock, now := newMockStore(t)
	command := rejudgeCommand(now)
	old := storedSubmission(now, submissionv1.SubmissionStatus_SUBMISSION_STATUS_DONE)
	old.Verdict = submissionv1.JudgeVerdict_JUDGE_VERDICT_AC

	mock.ExpectBegin()
	expectIdempotencyReservation(mock, command.Idempotency, now, 1)
	mock.ExpectQuery("SELECT " + regexp.QuoteMeta(submissionColumns) + " FROM submissions WHERE id = \\? FOR UPDATE").
		WithArgs(int64(44)).WillReturnRows(submissionRow(old))
	mock.ExpectExec("UPDATE submissions").
		WithArgs("INVALIDATED", now, now, int64(44), "DONE").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO outbox_events").
		WithArgs(invalidatedEvent, int64(44), biz.EventTypeSubmissionInvalidated, jsonArgument{biz.SubmissionInvalidatedPayload{
			SubmissionID: 44, UserID: 5, ProblemID: 7, PreviousVerdict: "AC", InvalidatedAt: now,
		}}, now).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO submissions").WillReturnResult(sqlmock.NewResult(45, 1))
	mock.ExpectExec("INSERT INTO outbox_events").
		WithArgs(secondRequested, int64(45), biz.EventTypeJudgeRequested, jsonArgument{biz.JudgeRequestedPayload{
			SubmissionID: 45, ProblemID: 7, Language: "go", JudgeRevision: dataTestRevision,
			SourceObjectKey: "sources/id/source.go", SourceSHA256: strings.Repeat("a", 64), SourceSizeBytes: 13,
		}}, now).WillReturnResult(sqlmock.NewResult(2, 1))
	mock.ExpectExec("UPDATE idempotency_requests").
		WithArgs(jsonArgument{map[string]any{"invalidated_submission_id": int64(44), "submission_id": int64(45)}}, int64(9), biz.OperationRejudgeSubmission, rejudgeKey).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	result, err := store.InvalidateAndRequeueWithOutboxAndIdempotency(context.Background(), command)
	if err != nil {
		t.Fatalf("InvalidateAndRequeueWithOutboxAndIdempotency() error = %v", err)
	}
	if result.InvalidatedSubmissionID != 44 || result.Submission.ID != 45 || result.Submission.SourceObjectKey != old.SourceObjectKey || result.Submission.JudgeRevision != dataTestRevision {
		t.Fatalf("result = %+v", result)
	}
	assertExpectations(t, mock)
}

func TestStoreRejudgeRejectsInvalidatedSubmission(t *testing.T) {
	store, mock, now := newMockStore(t)
	command := rejudgeCommand(now)
	mock.ExpectBegin()
	expectIdempotencyReservation(mock, command.Idempotency, now, 1)
	mock.ExpectQuery("SELECT " + regexp.QuoteMeta(submissionColumns) + " FROM submissions WHERE id = \\? FOR UPDATE").
		WithArgs(int64(44)).WillReturnRows(submissionRow(storedSubmission(now, submissionv1.SubmissionStatus_SUBMISSION_STATUS_INVALIDATED)))
	mock.ExpectRollback()

	_, err := store.InvalidateAndRequeueWithOutboxAndIdempotency(context.Background(), command)
	if !biz.HasReason(err, biz.ReasonConcurrentRejudge) {
		t.Fatalf("error = %v, want concurrent rejudge", err)
	}
	assertExpectations(t, mock)
}

func TestStoreListUsesStableDescendingOrder(t *testing.T) {
	store, mock, now := newMockStore(t)
	filter := biz.ListFilter{UserID: 5, Status: submissionv1.SubmissionStatus_SUBMISSION_STATUS_DONE, Language: " Go ", Page: 2, PageSize: 10}
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM submissions WHERE user_id = \\? AND status = \\? AND language = \\?").
		WithArgs(int64(5), "DONE", "go").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery("ORDER BY created_at DESC, id DESC LIMIT \\? OFFSET \\?").
		WithArgs(int64(5), "DONE", "go", int32(10), int64(10)).
		WillReturnRows(submissionRow(storedSubmission(now, submissionv1.SubmissionStatus_SUBMISSION_STATUS_DONE)))
	page, err := store.List(context.Background(), filter)
	if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Page != 2 || page.PageSize != 10 {
		t.Fatalf("List() = %+v, error = %v", page, err)
	}
	assertExpectations(t, mock)
}

func TestStoreGetJudgeResultMapsNullableFieldsAndOrdersCases(t *testing.T) {
	store, mock, now := newMockStore(t)
	submission := storedSubmission(now, submissionv1.SubmissionStatus_SUBMISSION_STATUS_DONE)
	submission.Verdict = submissionv1.JudgeVerdict_JUDGE_VERDICT_WA
	mock.ExpectQuery("SELECT " + regexp.QuoteMeta(submissionColumns) + " FROM submissions WHERE id = \\?").
		WithArgs(int64(44)).WillReturnRows(submissionRow(submission))
	mock.ExpectQuery("FROM submission_case_results WHERE submission_id = \\? ORDER BY case_no ASC").
		WithArgs(int64(44)).WillReturnRows(sqlmock.NewRows([]string{
		"id", "submission_id", "case_no", "verdict", "time_ms", "memory_kb", "message", "created_at",
	}).AddRow(1, 44, 1, "AC", 12, 1024, nil, now).AddRow(2, 44, 2, "WA", nil, nil, "mismatch", now))

	result, err := store.GetJudgeResult(context.Background(), 44)
	if err != nil {
		t.Fatalf("GetJudgeResult() error = %v", err)
	}
	if len(result.Cases) != 2 || result.Cases[0].CaseNo != 1 || result.Cases[1].CaseNo != 2 {
		t.Fatalf("cases = %+v", result.Cases)
	}
	if result.Cases[0].TimeMS == nil || *result.Cases[0].TimeMS != 12 || result.Cases[1].TimeMS != nil || result.Cases[1].Message != "mismatch" {
		t.Fatalf("nullable case fields = %+v", result.Cases)
	}
	assertExpectations(t, mock)
}

func TestStoreApplyJudgeResultUsesConsumerIdentityAndCanonicalStatus(t *testing.T) {
	store, mock, now := newMockStore(t)
	event := biz.JudgeResultEvent{
		EventID: "123e4567-e89b-12d3-a456-426614174005", EventType: biz.EventTypeJudgeCompleted,
		EventVersion: 1, SubmissionID: 44, JudgeRevision: dataTestRevision,
		Verdict: submissionv1.JudgeVerdict_JUDGE_VERDICT_AC, OccurredAt: now,
	}
	mock.ExpectBegin()
	mock.ExpectExec("INSERT IGNORE INTO processed_events \\(consumer_name, event_id, processed_at\\)").
		WithArgs(resultConsumerName, event.EventID, now).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("SELECT user_id, problem_id, judge_revision, status FROM submissions WHERE id = \\? FOR UPDATE").
		WithArgs(int64(44)).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "problem_id", "judge_revision", "status"}).AddRow(5, 7, dataTestRevision, "RUNNING"))
	mock.ExpectExec("UPDATE submissions SET status = \\?, verdict = \\?").
		WithArgs("DONE", "AC", nil, nil, now, now, int64(44)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO outbox_events").
		WithArgs(sqlmock.AnyArg(), int64(44), biz.EventTypeSubmissionJudged, jsonArgument{biz.SubmissionJudgedPayload{
			SubmissionID: 44, UserID: 5, ProblemID: 7, Verdict: "AC", JudgedAt: now,
		}}, now).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if err := store.ApplyJudgeResult(context.Background(), event); err != nil {
		t.Fatalf("ApplyJudgeResult() error = %v", err)
	}
	assertExpectations(t, mock)
}

func TestStoreApplyJudgeResultDoesNotOverwriteInvalidatedSubmission(t *testing.T) {
	store, mock, now := newMockStore(t)
	event := biz.JudgeResultEvent{
		EventID: "123e4567-e89b-12d3-a456-426614174006", EventType: biz.EventTypeJudgeCompleted,
		EventVersion: 1, SubmissionID: 44, JudgeRevision: dataTestRevision,
		Verdict: submissionv1.JudgeVerdict_JUDGE_VERDICT_AC, OccurredAt: now,
	}
	mock.ExpectBegin()
	mock.ExpectExec("INSERT IGNORE INTO processed_events \\(consumer_name, event_id, processed_at\\)").
		WithArgs(resultConsumerName, event.EventID, now).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("SELECT user_id, problem_id, judge_revision, status FROM submissions WHERE id = \\? FOR UPDATE").
		WithArgs(int64(44)).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "problem_id", "judge_revision", "status"}).AddRow(5, 7, dataTestRevision, "INVALIDATED"))
	mock.ExpectCommit()

	if err := store.ApplyJudgeResult(context.Background(), event); err != nil {
		t.Fatalf("ApplyJudgeResult() error = %v", err)
	}
	assertExpectations(t, mock)
}

func newMockStore(t *testing.T) (*StoreSet, sqlmock.Sqlmock, time.Time) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	now := time.Date(2026, 9, 18, 2, 3, 4, 5000000, time.UTC)
	return &StoreSet{db: db, clock: func() time.Time { return now }}, mock, now
}

func createCommand(now time.Time) biz.CreateSubmissionCommand {
	return biz.CreateSubmissionCommand{
		Submission: biz.Submission{
			UserID: 5, ProblemID: 7, Language: "go", SourceObjectKey: "sources/id/source.go",
			SourceSHA256: strings.Repeat("a", 64), SourceSizeBytes: 13,
			JudgeRevision: dataTestRevision, JudgeDeadlineAt: now.Add(time.Minute),
		},
		Idempotency: biz.IdempotencyRequest{
			ActorID: 5, Operation: biz.OperationCreateSubmission, Key: createKey,
			RequestHash: strings.Repeat("a", 64), ExpiresAt: now.Add(time.Hour),
		},
		OutboxEventID: requestedEvent,
	}
}

func rejudgeCommand(now time.Time) biz.RejudgeSubmissionCommand {
	return biz.RejudgeSubmissionCommand{
		SubmissionID: 44, JudgeRevision: dataTestRevision, JudgeDeadlineAt: now.Add(2 * time.Minute),
		Idempotency: biz.IdempotencyRequest{
			ActorID: 9, Operation: biz.OperationRejudgeSubmission, Key: rejudgeKey,
			RequestHash: strings.Repeat("c", 64), ExpiresAt: now.Add(time.Hour),
		},
		InvalidatedOutboxEventID: invalidatedEvent, RequestedOutboxEventID: secondRequested,
	}
}

func expectIdempotencyReservation(mock sqlmock.Sqlmock, request biz.IdempotencyRequest, now time.Time, affected int64) {
	expectation := mock.ExpectExec("INSERT INTO idempotency_requests").
		WithArgs(request.ActorID, request.Operation, request.Key, request.RequestHash, now, request.ExpiresAt)
	if affected == 1 {
		expectation.WillReturnResult(sqlmock.NewResult(1, 1))
		return
	}
	expectation.WillReturnError(&mysql.MySQLError{Number: 1062, Message: "duplicate idempotency key"})
}

func storedSubmission(now time.Time, status submissionv1.SubmissionStatus) biz.Submission {
	return biz.Submission{
		ID: 44, UserID: 5, ProblemID: 7, Language: "go", SourceObjectKey: "sources/id/source.go",
		SourceSHA256: strings.Repeat("a", 64), SourceSizeBytes: 13, JudgeRevision: dataTestRevision,
		Status: status, JudgeDeadlineAt: now.Add(time.Minute), CreatedAt: now.Add(-time.Minute), UpdatedAt: now,
	}
}

func submissionRow(submission biz.Submission) *sqlmock.Rows {
	var verdict any
	if submission.Verdict != submissionv1.JudgeVerdict_JUDGE_VERDICT_UNSPECIFIED {
		verdict = verdictToDB(submission.Verdict)
	}
	return sqlmock.NewRows([]string{
		"id", "user_id", "problem_id", "language", "source_object_key", "source_sha256", "source_size_bytes",
		"judge_revision", "status", "verdict", "time_ms", "memory_kb", "retry_count", "system_error_reason",
		"judge_deadline_at", "created_at", "judged_at", "invalidated_at", "updated_at",
	}).AddRow(
		submission.ID, submission.UserID, submission.ProblemID, submission.Language, submission.SourceObjectKey,
		submission.SourceSHA256, submission.SourceSizeBytes, submission.JudgeRevision, statusToDB(submission.Status), verdict,
		nil, nil, submission.RetryCount, nil, submission.JudgeDeadlineAt, submission.CreatedAt, nil, nil, submission.UpdatedAt,
	)
}

func assertExpectations(t *testing.T, mock sqlmock.Sqlmock) {
	t.Helper()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

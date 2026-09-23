package data

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	mysql "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"

	submissionv1 "github.com/viggggil/go_oj_agent/api/submission/v1"
	"github.com/viggggil/go_oj_agent/services/judge/internal/biz"
)

const resultConsumerName = "judge-result-consumer"

func (s *StoreSet) ApplyJudgeResult(ctx context.Context, event biz.JudgeResultEvent) (err error) {
	if s == nil || s.db == nil {
		return biz.ErrorInternal("submission repository is not configured")
	}
	if err = event.Validate(); err != nil {
		return biz.ErrorInvalidArgument("invalid judge result: %s", err.Error())
	}
	now := s.now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return storageError(err)
	}
	defer rollbackOnError(tx, &err)
	result, err := tx.ExecContext(ctx, `INSERT IGNORE INTO processed_events (consumer_name, event_id, processed_at) VALUES (?, ?, ?)`, resultConsumerName, event.EventID, now)
	if err != nil {
		return storageError(err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return storageError(err)
	}
	if affected == 0 {
		if err = tx.Commit(); err != nil {
			return storageError(err)
		}
		return nil
	}
	var userID, problemID int64
	var revision, status string
	if err = tx.QueryRowContext(ctx, `SELECT user_id, problem_id, judge_revision, status FROM submissions WHERE id = ? FOR UPDATE`, event.SubmissionID).Scan(&userID, &problemID, &revision, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return biz.ErrorSubmissionNotFound()
		}
		return storageError(err)
	}
	if revision != event.JudgeRevision {
		return biz.ErrorInvalidArgument("judge result revision does not match submission")
	}
	if status == statusToDB(submissionv1.SubmissionStatus_SUBMISSION_STATUS_DONE) ||
		status == statusToDB(submissionv1.SubmissionStatus_SUBMISSION_STATUS_INVALIDATED) ||
		status == statusToDB(submissionv1.SubmissionStatus_SUBMISSION_STATUS_CANCELLED) {
		if err = tx.Commit(); err != nil {
			return storageError(err)
		}
		return nil
	}
	if event.EventType == biz.EventTypeJudgeCompleted {
		if _, err = tx.ExecContext(ctx, `UPDATE submissions SET status = ?, verdict = ?, time_ms = ?, memory_kb = ?, judged_at = ?, updated_at = ?, system_error_reason = NULL WHERE id = ?`, statusToDB(submissionv1.SubmissionStatus_SUBMISSION_STATUS_DONE), verdictToDB(event.Verdict), nullableInt32Value(event.TimeMS), nullableInt32Value(event.MemoryKB), event.OccurredAt, now, event.SubmissionID); err != nil {
			return storageError(err)
		}
		for _, item := range event.CaseResults {
			if _, err = tx.ExecContext(ctx, `INSERT INTO submission_case_results (submission_id, case_no, verdict, time_ms, memory_kb, message, created_at) VALUES (?, ?, ?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE verdict=VALUES(verdict), time_ms=VALUES(time_ms), memory_kb=VALUES(memory_kb), message=VALUES(message)`, event.SubmissionID, item.CaseNo, verdictToDB(item.Verdict), nullableInt32Value(item.TimeMS), nullableInt32Value(item.MemoryKB), nullableString(item.Message), now); err != nil {
				return storageError(err)
			}
		}
	} else if event.Retryable {
		if _, err = tx.ExecContext(ctx, `UPDATE submissions SET status = ?, retry_count = LEAST(retry_count + 1, 3), system_error_reason = ?, updated_at = ? WHERE id = ?`, statusToDB(submissionv1.SubmissionStatus_SUBMISSION_STATUS_RETRY_WAIT), event.Reason, now, event.SubmissionID); err != nil {
			return storageError(err)
		}
		if err = tx.Commit(); err != nil {
			return storageError(err)
		}
		return nil
	} else {
		if _, err = tx.ExecContext(ctx, `UPDATE submissions SET status = ?, system_error_reason = ?, judged_at = ?, updated_at = ? WHERE id = ?`, statusToDB(submissionv1.SubmissionStatus_SUBMISSION_STATUS_DONE), event.Reason, event.OccurredAt, now, event.SubmissionID); err != nil {
			return storageError(err)
		}
	}
	payload := biz.SubmissionJudgedPayload{SubmissionID: event.SubmissionID, UserID: userID, ProblemID: problemID, Verdict: verdictToDB(event.Verdict), JudgedAt: event.OccurredAt}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return biz.ErrorInternal("judge result event cannot be encoded")
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO outbox_events (event_id, aggregate_type, aggregate_id, event_type, event_version, payload, status, retry_count, created_at) VALUES (?, 'submission', ?, ?, 1, ?, 'pending', 0, ?)`, uuid.NewString(), event.SubmissionID, biz.EventTypeSubmissionJudged, encoded, now)
	if err != nil {
		return storageError(err)
	}
	if err = tx.Commit(); err != nil {
		return storageError(err)
	}
	return nil
}

const submissionColumns = `id, user_id, problem_id, language, source_object_key,
	source_sha256, source_size_bytes, judge_revision, status, verdict, time_ms,
	memory_kb, retry_count, system_error_reason, judge_deadline_at, created_at,
	judged_at, invalidated_at, updated_at`

type StoreSet struct {
	db    *sql.DB
	clock func() time.Time
}

func NewStoreSet(db *sql.DB) *StoreSet {
	return &StoreSet{db: db, clock: func() time.Time { return time.Now().UTC() }}
}

func (s *StoreSet) FindByID(ctx context.Context, id int64) (biz.Submission, error) {
	if s == nil || s.db == nil {
		return biz.Submission{}, biz.ErrorInternal("submission repository is not configured")
	}
	return scanSubmission(s.db.QueryRowContext(ctx, `SELECT `+submissionColumns+` FROM submissions WHERE id = ?`, id))
}

func (s *StoreSet) List(ctx context.Context, filter biz.ListFilter) (biz.SubmissionPage, error) {
	if s == nil || s.db == nil {
		return biz.SubmissionPage{}, biz.ErrorInternal("submission repository is not configured")
	}
	filter = filter.Normalized()
	where, args := listPredicate(filter)
	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM submissions`+where, args...).Scan(&total); err != nil {
		return biz.SubmissionPage{}, storageError(err)
	}
	offset := (int64(filter.Page) - 1) * int64(filter.PageSize)
	queryArgs := append(append([]any(nil), args...), filter.PageSize, offset)
	rows, err := s.db.QueryContext(ctx, `SELECT `+submissionColumns+` FROM submissions`+where+` ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return biz.SubmissionPage{}, storageError(err)
	}
	defer rows.Close()
	items := make([]biz.Submission, 0)
	for rows.Next() {
		item, scanErr := scanSubmission(rows)
		if scanErr != nil {
			return biz.SubmissionPage{}, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return biz.SubmissionPage{}, storageError(err)
	}
	return biz.SubmissionPage{Items: items, Page: filter.Page, PageSize: filter.PageSize, Total: total}, nil
}

func listPredicate(filter biz.ListFilter) (string, []any) {
	conditions := make([]string, 0, 4)
	args := make([]any, 0, 4)
	if filter.UserID > 0 {
		conditions = append(conditions, "user_id = ?")
		args = append(args, filter.UserID)
	}
	if filter.ProblemID > 0 {
		conditions = append(conditions, "problem_id = ?")
		args = append(args, filter.ProblemID)
	}
	if filter.Status != submissionv1.SubmissionStatus_SUBMISSION_STATUS_UNSPECIFIED {
		conditions = append(conditions, "status = ?")
		args = append(args, statusToDB(filter.Status))
	}
	if filter.Language != "" {
		conditions = append(conditions, "language = ?")
		args = append(args, filter.Language)
	}
	if len(conditions) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(conditions, " AND "), args
}

func (s *StoreSet) GetJudgeResult(ctx context.Context, id int64) (biz.JudgeResult, error) {
	submission, err := s.FindByID(ctx, id)
	if err != nil {
		return biz.JudgeResult{}, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, submission_id, case_no, verdict, time_ms, memory_kb, message, created_at
		FROM submission_case_results
		WHERE submission_id = ?
		ORDER BY case_no ASC
	`, id)
	if err != nil {
		return biz.JudgeResult{}, storageError(err)
	}
	defer rows.Close()
	cases := make([]biz.CaseResult, 0)
	for rows.Next() {
		var item biz.CaseResult
		var verdict string
		var timeMS, memoryKB sql.NullInt32
		var message sql.NullString
		if err := rows.Scan(&item.ID, &item.SubmissionID, &item.CaseNo, &verdict, &timeMS, &memoryKB, &message, &item.CreatedAt); err != nil {
			return biz.JudgeResult{}, storageError(err)
		}
		item.Verdict, err = verdictFromDB(verdict)
		if err != nil {
			return biz.JudgeResult{}, err
		}
		item.TimeMS = nullableInt32(timeMS)
		item.MemoryKB = nullableInt32(memoryKB)
		item.Message = message.String
		cases = append(cases, item)
	}
	if err := rows.Err(); err != nil {
		return biz.JudgeResult{}, storageError(err)
	}
	return biz.JudgeResult{Submission: submission, Cases: cases}, nil
}

func (s *StoreSet) FindIdempotency(ctx context.Context, actorID int64, operation, key string) (biz.IdempotencyRecord, bool, error) {
	if s == nil || s.db == nil {
		return biz.IdempotencyRecord{}, false, biz.ErrorInternal("submission repository is not configured")
	}
	var record biz.IdempotencyRecord
	var response []byte
	err := s.db.QueryRowContext(ctx, `
		SELECT request_hash, response, created_at, expires_at
		FROM idempotency_requests
		WHERE actor_id = ? AND operation = ? AND idempotency_key = ?
	`, actorID, operation, key).Scan(&record.RequestHash, &response, &record.CreatedAt, &record.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return biz.IdempotencyRecord{}, false, nil
	}
	if err != nil {
		return biz.IdempotencyRecord{}, false, storageError(err)
	}
	record.ActorID = actorID
	record.Operation = operation
	record.Key = key
	record.Response = append([]byte(nil), response...)
	return record, true, nil
}

func (s *StoreSet) CreateWithOutboxAndIdempotency(ctx context.Context, command biz.CreateSubmissionCommand) (result biz.CreateSubmissionResult, err error) {
	if s == nil || s.db == nil {
		return result, biz.ErrorInternal("submission repository is not configured")
	}
	now := s.now()
	if err := biz.ValidateSubmissionForCreate(command.Submission); err != nil {
		return result, err
	}
	if !command.Submission.JudgeDeadlineAt.After(now) {
		return result, biz.ErrorInvalidArgument("judge deadline must be in the future")
	}
	if err := biz.ValidateIdempotency(command.Idempotency, biz.OperationCreateSubmission, now); err != nil {
		return result, err
	}
	if err := biz.ValidateEventID(command.OutboxEventID); err != nil {
		return result, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, storageError(err)
	}
	defer rollbackOnError(tx, &err)

	created, response, err := reserveIdempotency(ctx, tx, command.Idempotency, now)
	if err != nil {
		return result, err
	}
	if !created {
		if err := json.Unmarshal(response, &result); err != nil {
			return result, biz.ErrorInternal("stored idempotency response is invalid")
		}
		result.Replayed = true
		if err = tx.Commit(); err != nil {
			return biz.CreateSubmissionResult{}, storageError(err)
		}
		return result, nil
	}

	submission := command.Submission
	submission.Status = submissionv1.SubmissionStatus_SUBMISSION_STATUS_QUEUED
	submission.CreatedAt = now
	submission.UpdatedAt = now
	submission.ID, err = insertSubmission(ctx, tx, submission)
	if err != nil {
		return result, err
	}
	if err = insertJudgeRequestedOutbox(ctx, tx, command.OutboxEventID, submission, submission.CreatedAt); err != nil {
		return result, err
	}
	result = biz.CreateSubmissionResult{SubmissionID: submission.ID, Status: submission.Status}
	if err = completeIdempotency(ctx, tx, command.Idempotency, result); err != nil {
		return result, err
	}
	if err = tx.Commit(); err != nil {
		return biz.CreateSubmissionResult{}, storageError(err)
	}
	return result, nil
}

func (s *StoreSet) InvalidateAndRequeueWithOutboxAndIdempotency(ctx context.Context, command biz.RejudgeSubmissionCommand) (result biz.RejudgeSubmissionResult, err error) {
	if s == nil || s.db == nil {
		return result, biz.ErrorInternal("submission repository is not configured")
	}
	now := s.now()
	if command.SubmissionID <= 0 || command.JudgeDeadlineAt.IsZero() {
		return result, biz.ErrorInvalidArgument("invalid rejudge submission")
	}
	if !command.JudgeDeadlineAt.After(now) {
		return result, biz.ErrorInvalidArgument("judge deadline must be in the future")
	}
	if err := biz.ValidateJudgeRevision(command.JudgeRevision); err != nil {
		return result, err
	}
	if err := biz.ValidateIdempotency(command.Idempotency, biz.OperationRejudgeSubmission, now); err != nil {
		return result, err
	}
	if err := biz.ValidateEventID(command.InvalidatedOutboxEventID); err != nil {
		return result, err
	}
	if err := biz.ValidateEventID(command.RequestedOutboxEventID); err != nil {
		return result, err
	}
	if command.InvalidatedOutboxEventID == command.RequestedOutboxEventID {
		return result, biz.ErrorInvalidArgument("rejudge outbox event ids must be distinct")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, storageError(err)
	}
	defer rollbackOnError(tx, &err)

	created, response, err := reserveIdempotency(ctx, tx, command.Idempotency, now)
	if err != nil {
		return result, err
	}
	if !created {
		var replay struct {
			InvalidatedSubmissionID int64 `json:"invalidated_submission_id"`
			SubmissionID            int64 `json:"submission_id"`
		}
		if err := json.Unmarshal(response, &replay); err != nil {
			return result, biz.ErrorInternal("stored idempotency response is invalid")
		}
		replacement, findErr := scanSubmission(tx.QueryRowContext(ctx, `SELECT `+submissionColumns+` FROM submissions WHERE id = ?`, replay.SubmissionID))
		if findErr != nil {
			return result, findErr
		}
		result = biz.RejudgeSubmissionResult{InvalidatedSubmissionID: replay.InvalidatedSubmissionID, Submission: replacement, Replayed: true}
		if err = tx.Commit(); err != nil {
			return biz.RejudgeSubmissionResult{}, storageError(err)
		}
		return result, nil
	}

	old, err := scanSubmission(tx.QueryRowContext(ctx, `SELECT `+submissionColumns+` FROM submissions WHERE id = ? FOR UPDATE`, command.SubmissionID))
	if err != nil {
		return result, err
	}
	if !biz.CanInvalidate(old.Status) {
		if old.Status == submissionv1.SubmissionStatus_SUBMISSION_STATUS_INVALIDATED {
			return result, biz.ErrorConcurrentRejudge()
		}
		return result, biz.ErrorInvalidTransition("submission in %s cannot be rejudged", old.Status.String())
	}
	update, err := tx.ExecContext(ctx, `
		UPDATE submissions
		SET status = ?, invalidated_at = ?, updated_at = ?
		WHERE id = ? AND status = ?
	`, statusToDB(submissionv1.SubmissionStatus_SUBMISSION_STATUS_INVALIDATED), now, now, old.ID, statusToDB(old.Status))
	if err != nil {
		return result, storageError(err)
	}
	affected, err := update.RowsAffected()
	if err != nil {
		return result, storageError(err)
	}
	if affected != 1 {
		return result, biz.ErrorConcurrentRejudge()
	}
	if err = insertInvalidatedOutbox(ctx, tx, command.InvalidatedOutboxEventID, old, now); err != nil {
		return result, err
	}

	replacement := biz.Submission{
		UserID: old.UserID, ProblemID: old.ProblemID, Language: old.Language,
		SourceObjectKey: old.SourceObjectKey, SourceSHA256: old.SourceSHA256, SourceSizeBytes: old.SourceSizeBytes,
		JudgeRevision: command.JudgeRevision, Status: submissionv1.SubmissionStatus_SUBMISSION_STATUS_QUEUED,
		JudgeDeadlineAt: command.JudgeDeadlineAt, CreatedAt: now, UpdatedAt: now,
	}
	replacement.ID, err = insertSubmission(ctx, tx, replacement)
	if err != nil {
		return result, err
	}
	if err = insertJudgeRequestedOutbox(ctx, tx, command.RequestedOutboxEventID, replacement, now); err != nil {
		return result, err
	}
	storedResponse := struct {
		InvalidatedSubmissionID int64 `json:"invalidated_submission_id"`
		SubmissionID            int64 `json:"submission_id"`
	}{old.ID, replacement.ID}
	if err = completeIdempotency(ctx, tx, command.Idempotency, storedResponse); err != nil {
		return result, err
	}
	if err = tx.Commit(); err != nil {
		return biz.RejudgeSubmissionResult{}, storageError(err)
	}
	return biz.RejudgeSubmissionResult{InvalidatedSubmissionID: old.ID, Submission: replacement}, nil
}

func (s *StoreSet) ClaimOutbox(ctx context.Context, owner string, now, leaseUntil time.Time, limit int) (events []biz.OutboxEvent, err error) {
	if s == nil || s.db == nil {
		return nil, biz.ErrorInternal("submission repository is not configured")
	}
	if strings.TrimSpace(owner) == "" || limit <= 0 || !leaseUntil.After(now) {
		return nil, biz.ErrorInvalidArgument("invalid outbox lease request")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, storageError(err)
	}
	defer rollbackOnError(tx, &err)
	rows, err := tx.QueryContext(ctx, `
		SELECT id, event_id, aggregate_type, aggregate_id, event_type, event_version,
		       payload, status, retry_count, next_retry_at, lease_owner, lease_until,
		       created_at, published_at
		FROM outbox_events
		WHERE status = ?
		  AND (next_retry_at IS NULL OR next_retry_at <= ?)
		  AND (lease_until IS NULL OR lease_until <= ?)
		ORDER BY id ASC
		LIMIT ?
		FOR UPDATE SKIP LOCKED
	`, biz.OutboxStatusPending, now, now, limit)
	if err != nil {
		return nil, storageError(err)
	}
	for rows.Next() {
		event, scanErr := scanOutbox(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, scanErr
		}
		event.LeaseOwner = owner
		event.LeaseUntil = &leaseUntil
		events = append(events, event)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return nil, storageError(err)
	}
	if err = rows.Close(); err != nil {
		return nil, storageError(err)
	}
	for _, event := range events {
		if _, err = tx.ExecContext(ctx, `
			UPDATE outbox_events
			SET lease_owner = ?, lease_until = ?
			WHERE id = ? AND status = ?
		`, owner, leaseUntil, event.ID, biz.OutboxStatusPending); err != nil {
			return nil, storageError(err)
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, storageError(err)
	}
	return events, nil
}

func (s *StoreSet) MarkOutboxPublished(ctx context.Context, id int64, owner string, publishedAt time.Time) error {
	if s == nil || s.db == nil {
		return biz.ErrorInternal("submission repository is not configured")
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE outbox_events
		SET status = ?, published_at = ?, lease_owner = NULL, lease_until = NULL
		WHERE id = ? AND status = ? AND lease_owner = ?
	`, biz.OutboxStatusPublished, publishedAt, id, biz.OutboxStatusPending, owner)
	if err != nil {
		return storageError(err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return storageError(err)
	}
	if affected != 1 {
		return biz.ErrorInternal("outbox lease was lost before publish confirmation")
	}
	return nil
}

func (s *StoreSet) MarkOutboxFailure(ctx context.Context, id int64, owner string, nextRetryAt time.Time, dead bool, reason string) error {
	if s == nil || s.db == nil {
		return biz.ErrorInternal("submission repository is not configured")
	}
	status := biz.OutboxStatusPending
	if dead {
		status = biz.OutboxStatusDead
	}
	var retryAt any = nextRetryAt
	if dead {
		retryAt = nil
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE outbox_events
		SET status = ?, retry_count = retry_count + 1, next_retry_at = ?,
		    lease_owner = NULL, lease_until = NULL, last_error = ?
		WHERE id = ? AND status = ? AND lease_owner = ?
	`, status, retryAt, reason, id, biz.OutboxStatusPending, owner)
	if err != nil {
		return storageError(err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return storageError(err)
	}
	if affected != 1 {
		return biz.ErrorInternal("outbox lease was lost before failure update")
	}
	return nil
}

func insertSubmission(ctx context.Context, tx *sql.Tx, submission biz.Submission) (int64, error) {
	result, err := tx.ExecContext(ctx, `
		INSERT INTO submissions (
			user_id, problem_id, language, source_object_key, source_sha256,
			source_size_bytes, judge_revision, status, verdict, time_ms, memory_kb,
			retry_count, system_error_reason, judge_deadline_at, created_at, judged_at,
			invalidated_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, submission.UserID, submission.ProblemID, submission.Language, submission.SourceObjectKey,
		submission.SourceSHA256, submission.SourceSizeBytes, submission.JudgeRevision, statusToDB(submission.Status),
		nullableVerdict(submission.Verdict), nullableInt32Value(submission.TimeMS), nullableInt32Value(submission.MemoryKB),
		submission.RetryCount, nullableString(submission.SystemErrorReason), submission.JudgeDeadlineAt,
		submission.CreatedAt, submission.JudgedAt, submission.InvalidatedAt, submission.UpdatedAt)
	if err != nil {
		return 0, storageError(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, storageError(err)
	}
	return id, nil
}

func insertJudgeRequestedOutbox(ctx context.Context, tx *sql.Tx, eventID string, submission biz.Submission, now time.Time) error {
	payload := biz.JudgeRequestedPayload{
		SubmissionID: submission.ID, ProblemID: submission.ProblemID, Language: submission.Language,
		JudgeRevision: submission.JudgeRevision, SourceObjectKey: submission.SourceObjectKey,
		SourceSHA256: submission.SourceSHA256, SourceSizeBytes: submission.SourceSizeBytes,
	}
	return insertOutbox(ctx, tx, eventID, submission.ID, biz.EventTypeJudgeRequested, payload, now)
}

func insertInvalidatedOutbox(ctx context.Context, tx *sql.Tx, eventID string, submission biz.Submission, now time.Time) error {
	payload := biz.SubmissionInvalidatedPayload{
		SubmissionID: submission.ID, UserID: submission.UserID, ProblemID: submission.ProblemID,
		PreviousVerdict: verdictToDB(submission.Verdict), InvalidatedAt: now,
	}
	return insertOutbox(ctx, tx, eventID, submission.ID, biz.EventTypeSubmissionInvalidated, payload, now)
}

func insertOutbox(ctx context.Context, tx *sql.Tx, eventID string, aggregateID int64, eventType string, payload any, now time.Time) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return biz.ErrorInternal("outbox payload cannot be encoded")
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO outbox_events (
			event_id, aggregate_type, aggregate_id, event_type, event_version,
			payload, status, retry_count, created_at
		) VALUES (?, 'submission', ?, ?, 1, ?, 'pending', 0, ?)
	`, eventID, aggregateID, eventType, encoded, now)
	if err != nil {
		return storageError(err)
	}
	return nil
}

func reserveIdempotency(ctx context.Context, tx *sql.Tx, request biz.IdempotencyRequest, now time.Time) (bool, []byte, error) {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO idempotency_requests (
			actor_id, operation, idempotency_key, request_hash, response, created_at, expires_at
		) VALUES (?, ?, ?, ?, NULL, ?, ?)
	`, request.ActorID, request.Operation, request.Key, request.RequestHash, now, request.ExpiresAt)
	if err == nil {
		return true, nil, nil
	}
	if !isDuplicateKey(err) {
		return false, nil, storageError(err)
	}
	var storedHash string
	var response []byte
	err = tx.QueryRowContext(ctx, `
		SELECT request_hash, response
		FROM idempotency_requests
		WHERE actor_id = ? AND operation = ? AND idempotency_key = ?
		FOR UPDATE
	`, request.ActorID, request.Operation, request.Key).Scan(&storedHash, &response)
	if err != nil {
		return false, nil, storageError(err)
	}
	if storedHash != request.RequestHash {
		return false, nil, biz.ErrorIdempotencyConflict()
	}
	if len(response) == 0 {
		return false, nil, biz.ErrorIdempotencyInProgress()
	}
	return false, response, nil
}

func isDuplicateKey(err error) bool {
	var mysqlError *mysql.MySQLError
	return errors.As(err, &mysqlError) && mysqlError.Number == 1062
}

func completeIdempotency(ctx context.Context, tx *sql.Tx, request biz.IdempotencyRequest, response any) error {
	encoded, err := json.Marshal(response)
	if err != nil {
		return biz.ErrorInternal("idempotency response cannot be encoded")
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE idempotency_requests
		SET response = ?
		WHERE actor_id = ? AND operation = ? AND idempotency_key = ? AND response IS NULL
	`, encoded, request.ActorID, request.Operation, request.Key)
	if err != nil {
		return storageError(err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return storageError(err)
	}
	if affected != 1 {
		return biz.ErrorIdempotencyInProgress()
	}
	return nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanOutbox(row rowScanner) (biz.OutboxEvent, error) {
	var event biz.OutboxEvent
	var payload []byte
	var nextRetryAt, leaseUntil, publishedAt sql.NullTime
	var leaseOwner sql.NullString
	if err := row.Scan(
		&event.ID, &event.EventID, &event.AggregateType, &event.AggregateID,
		&event.EventType, &event.EventVersion, &payload, &event.Status,
		&event.RetryCount, &nextRetryAt, &leaseOwner, &leaseUntil,
		&event.CreatedAt, &publishedAt,
	); err != nil {
		return biz.OutboxEvent{}, storageError(err)
	}
	event.Payload = append(json.RawMessage(nil), payload...)
	event.NextRetryAt = nullableTime(nextRetryAt)
	event.LeaseOwner = leaseOwner.String
	event.LeaseUntil = nullableTime(leaseUntil)
	event.PublishedAt = nullableTime(publishedAt)
	return event, nil
}

func scanSubmission(row rowScanner) (biz.Submission, error) {
	var submission biz.Submission
	var status string
	var verdict, systemReason sql.NullString
	var timeMS, memoryKB sql.NullInt32
	var judgedAt, invalidatedAt sql.NullTime
	err := row.Scan(
		&submission.ID, &submission.UserID, &submission.ProblemID, &submission.Language,
		&submission.SourceObjectKey, &submission.SourceSHA256, &submission.SourceSizeBytes,
		&submission.JudgeRevision, &status, &verdict, &timeMS, &memoryKB, &submission.RetryCount,
		&systemReason, &submission.JudgeDeadlineAt, &submission.CreatedAt, &judgedAt,
		&invalidatedAt, &submission.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return biz.Submission{}, biz.ErrorSubmissionNotFound()
	}
	if err != nil {
		return biz.Submission{}, storageError(err)
	}
	submission.Status, err = statusFromDB(status)
	if err != nil {
		return biz.Submission{}, err
	}
	if verdict.Valid {
		submission.Verdict, err = verdictFromDB(verdict.String)
		if err != nil {
			return biz.Submission{}, err
		}
	}
	submission.TimeMS = nullableInt32(timeMS)
	submission.MemoryKB = nullableInt32(memoryKB)
	submission.SystemErrorReason = systemReason.String
	submission.JudgedAt = nullableTime(judgedAt)
	submission.InvalidatedAt = nullableTime(invalidatedAt)
	return submission, nil
}

func statusToDB(status submissionv1.SubmissionStatus) string {
	return strings.TrimPrefix(status.String(), "SUBMISSION_STATUS_")
}

func statusFromDB(value string) (submissionv1.SubmissionStatus, error) {
	status, ok := submissionv1.SubmissionStatus_value["SUBMISSION_STATUS_"+strings.ToUpper(value)]
	if !ok || status == int32(submissionv1.SubmissionStatus_SUBMISSION_STATUS_UNSPECIFIED) {
		return submissionv1.SubmissionStatus_SUBMISSION_STATUS_UNSPECIFIED, biz.ErrorInternal("stored submission status is invalid")
	}
	return submissionv1.SubmissionStatus(status), nil
}

func verdictToDB(verdict submissionv1.JudgeVerdict) string {
	if verdict == submissionv1.JudgeVerdict_JUDGE_VERDICT_UNSPECIFIED {
		return ""
	}
	return strings.TrimPrefix(verdict.String(), "JUDGE_VERDICT_")
}

func verdictFromDB(value string) (submissionv1.JudgeVerdict, error) {
	verdict, ok := submissionv1.JudgeVerdict_value["JUDGE_VERDICT_"+strings.ToUpper(value)]
	if !ok || verdict == int32(submissionv1.JudgeVerdict_JUDGE_VERDICT_UNSPECIFIED) {
		return submissionv1.JudgeVerdict_JUDGE_VERDICT_UNSPECIFIED, biz.ErrorInternal("stored judge verdict is invalid")
	}
	return submissionv1.JudgeVerdict(verdict), nil
}

func nullableVerdict(verdict submissionv1.JudgeVerdict) any {
	if value := verdictToDB(verdict); value != "" {
		return value
	}
	return nil
}

func nullableInt32(value sql.NullInt32) *int32 {
	if !value.Valid {
		return nil
	}
	result := value.Int32
	return &result
}

func nullableInt32Value(value *int32) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableTime(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time
	return &result
}

func rollbackOnError(tx *sql.Tx, err *error) {
	if *err != nil {
		_ = tx.Rollback()
	}
}

func (s *StoreSet) now() time.Time {
	if s.clock == nil {
		return time.Now().UTC()
	}
	return s.clock().UTC()
}

func storageError(err error) error {
	if err == nil {
		return nil
	}
	return biz.ErrorInternal("submission storage operation failed")
}

var _ biz.SubmissionRepository = (*StoreSet)(nil)

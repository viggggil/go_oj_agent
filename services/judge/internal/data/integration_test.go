package data

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/oklog/ulid/v2"

	"github.com/viggggil/go_oj_agent/services/judge/internal/biz"
	"github.com/viggggil/go_oj_agent/services/judge/internal/conf"
)

func TestJudgeInfrastructureIntegration(t *testing.T) {
	mysqlDSN := os.Getenv("SUBMISSION_TEST_MYSQL_DSN")
	minioEndpoint := os.Getenv("PROBLEM_TEST_MINIO_ENDPOINT")
	if mysqlDSN == "" || minioEndpoint == "" {
		t.Skip("set SUBMISSION_TEST_MYSQL_DSN and PROBLEM_TEST_MINIO_ENDPOINT")
	}
	ctx := t.Context()
	db, err := sql.Open("mysql", mysqlDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	store := NewStoreSet(db)
	sources, err := NewSourceStore(&conf.Bootstrap{Storage: &conf.StorageProto{Minio: &conf.MinIOProto{
		Endpoint: minioEndpoint, AccessKey: integrationEnvOr("PROBLEM_TEST_MINIO_ACCESS_KEY", "minioadmin"),
		SecretKey: integrationEnvOr("PROBLEM_TEST_MINIO_SECRET_KEY", "minioadmin"), SourceBucket: "submission-source",
	}}})
	if err != nil {
		t.Fatal(err)
	}

	actorID := time.Now().UnixNano()%1_000_000_000 + 1_000_000
	problemID := actorID + 1
	var submissionIDs []int64
	var objectKeys []string
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM idempotency_requests WHERE actor_id IN (?, ?)`, actorID, actorID+10)
		for i := len(submissionIDs) - 1; i >= 0; i-- {
			_, _ = db.ExecContext(context.Background(), `DELETE FROM outbox_events WHERE aggregate_id = ?`, submissionIDs[i])
			_, _ = db.ExecContext(context.Background(), `DELETE FROM submissions WHERE id = ?`, submissionIDs[i])
		}
		client, _ := sources.client.(*minio.Client)
		if client != nil {
			for _, key := range objectKeys {
				_ = client.RemoveObject(context.Background(), sources.bucket, key, minio.RemoveObjectOptions{})
			}
		}
	})

	source, err := sources.Put(ctx, "go", []byte("package main\nfunc main() {}\n"))
	if err != nil {
		t.Fatal(err)
	}
	objectKeys = append(objectKeys, source.Key)
	client := sources.client.(*minio.Client)
	info, err := client.StatObject(ctx, sources.bucket, source.Key, minio.StatObjectOptions{})
	if err != nil || info.Size != source.Size {
		t.Fatalf("source StatObject size=%d err=%v", info.Size, err)
	}

	now := time.Now().UTC()
	create := biz.CreateSubmissionCommand{
		Submission: biz.Submission{
			UserID: actorID, ProblemID: problemID, Language: "go", SourceObjectKey: source.Key,
			SourceSHA256: source.SHA256, SourceSizeBytes: source.Size, JudgeRevision: ulid.Make().String(),
			JudgeDeadlineAt: now.Add(5 * time.Minute),
		},
		Idempotency: biz.IdempotencyRequest{
			ActorID: actorID, Operation: biz.OperationCreateSubmission, Key: uuid.NewString(),
			RequestHash: strings.Repeat("a", 64), ExpiresAt: now.Add(time.Hour),
		},
		OutboxEventID: uuid.NewString(),
	}
	created, err := store.CreateWithOutboxAndIdempotency(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	submissionIDs = append(submissionIDs, created.SubmissionID)

	var submissionCount, outboxCount, responseCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM submissions WHERE id = ?`, created.SubmissionID).Scan(&submissionCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE aggregate_id = ? AND event_type = ?`, created.SubmissionID, biz.EventTypeJudgeRequested).Scan(&outboxCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM idempotency_requests WHERE actor_id = ? AND operation = ? AND idempotency_key = ? AND response IS NOT NULL`, actorID, biz.OperationCreateSubmission, create.Idempotency.Key).Scan(&responseCount); err != nil {
		t.Fatal(err)
	}
	if submissionCount != 1 || outboxCount != 1 || responseCount != 1 {
		t.Fatalf("atomic create counts submission=%d outbox=%d response=%d", submissionCount, outboxCount, responseCount)
	}
	replayed, err := store.CreateWithOutboxAndIdempotency(ctx, create)
	if err != nil || !replayed.Replayed || replayed.SubmissionID != created.SubmissionID {
		t.Fatalf("create replay = %+v, %v", replayed, err)
	}

	failed := create
	failed.Idempotency.Key = uuid.NewString()
	failed.Idempotency.RequestHash = strings.Repeat("b", 64)
	failed.OutboxEventID = create.OutboxEventID
	before := countUserSubmissions(t, db, actorID)
	if _, err := store.CreateWithOutboxAndIdempotency(ctx, failed); !biz.HasReason(err, biz.ReasonInternal) {
		t.Fatalf("duplicate outbox error = %v", err)
	}
	after := countUserSubmissions(t, db, actorID)
	if after != before {
		t.Fatalf("failed transaction changed submission count from %d to %d", before, after)
	}
	var failedIdempotency int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM idempotency_requests WHERE actor_id = ? AND operation = ? AND idempotency_key = ?`, actorID, biz.OperationCreateSubmission, failed.Idempotency.Key).Scan(&failedIdempotency); err != nil || failedIdempotency != 0 {
		t.Fatalf("failed idempotency count=%d err=%v", failedIdempotency, err)
	}

	rejudgeBase := biz.RejudgeSubmissionCommand{
		SubmissionID: created.SubmissionID, JudgeRevision: ulid.Make().String(), JudgeDeadlineAt: time.Now().UTC().Add(5 * time.Minute),
		Idempotency: biz.IdempotencyRequest{
			ActorID: actorID + 10, Operation: biz.OperationRejudgeSubmission,
			RequestHash: strings.Repeat("c", 64), ExpiresAt: time.Now().UTC().Add(time.Hour),
		},
	}
	commands := []biz.RejudgeSubmissionCommand{rejudgeBase, rejudgeBase}
	for i := range commands {
		commands[i].Idempotency.Key = uuid.NewString()
		commands[i].Idempotency.RequestHash = fmt.Sprintf("%064x", i+1)
		commands[i].InvalidatedOutboxEventID = uuid.NewString()
		commands[i].RequestedOutboxEventID = uuid.NewString()
	}
	type outcome struct {
		result biz.RejudgeSubmissionResult
		err    error
	}
	outcomes := make(chan outcome, len(commands))
	var ready sync.WaitGroup
	ready.Add(len(commands))
	start := make(chan struct{})
	for _, command := range commands {
		go func() {
			ready.Done()
			<-start
			result, callErr := store.InvalidateAndRequeueWithOutboxAndIdempotency(context.Background(), command)
			outcomes <- outcome{result, callErr}
		}()
	}
	ready.Wait()
	close(start)
	var successes, conflicts int
	for range commands {
		outcome := <-outcomes
		if outcome.err == nil {
			successes++
			submissionIDs = append(submissionIDs, outcome.result.Submission.ID)
			if outcome.result.Submission.SourceObjectKey != source.Key {
				t.Fatalf("rejudge did not reuse source: %+v", outcome.result.Submission)
			}
		} else if biz.HasReason(outcome.err, biz.ReasonConcurrentRejudge) {
			conflicts++
		} else {
			t.Fatalf("unexpected rejudge error: %v", outcome.err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent rejudge successes=%d conflicts=%d", successes, conflicts)
	}
	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM submissions WHERE id = ?`, created.SubmissionID).Scan(&status); err != nil || status != "INVALIDATED" {
		t.Fatalf("old submission status=%q err=%v", status, err)
	}
	var invalidatedEvents, requestedEvents, completedRejudges int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE aggregate_id = ? AND event_type = ?`, created.SubmissionID, biz.EventTypeSubmissionInvalidated).Scan(&invalidatedEvents); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM outbox_events e
		INNER JOIN submissions s ON s.id = e.aggregate_id
		WHERE s.user_id = ? AND s.id <> ? AND e.event_type = ?
	`, actorID, created.SubmissionID, biz.EventTypeJudgeRequested).Scan(&requestedEvents); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM idempotency_requests WHERE actor_id = ? AND operation = ? AND response IS NOT NULL`, actorID+10, biz.OperationRejudgeSubmission).Scan(&completedRejudges); err != nil {
		t.Fatal(err)
	}
	if invalidatedEvents != 1 || requestedEvents != 1 || completedRejudges != 1 {
		t.Fatalf("atomic rejudge counts invalidated=%d requested=%d idempotency=%d", invalidatedEvents, requestedEvents, completedRejudges)
	}
}

func countUserSubmissions(t *testing.T, db *sql.DB, userID int64) int {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM submissions WHERE user_id = ?`, userID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func integrationEnvOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

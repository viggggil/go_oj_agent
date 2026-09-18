package integration_test

import (
	"database/sql"
	"errors"
	"os"
	"testing"

	_ "github.com/go-sql-driver/mysql"
)

func TestSubmissionSchema(t *testing.T) {
	dsn := os.Getenv("SUBMISSION_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set SUBMISSION_TEST_MYSQL_DSN to run the submission schema integration test")
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}

	assertColumns(t, db, "submissions", []string{
		"source_object_key",
		"source_sha256",
		"source_size_bytes",
		"judge_revision",
		"retry_count",
		"system_error_reason",
		"judge_deadline_at",
		"invalidated_at",
	})
	assertMissingColumns(t, db, "submissions", []string{"source_code", "testcase_version"})
	assertColumns(t, db, "outbox_events", []string{"lease_owner", "lease_until"})
	assertColumns(t, db, "processed_events", []string{"consumer_name", "event_id", "processed_at"})
	assertColumns(t, db, "idempotency_requests", []string{
		"actor_id",
		"operation",
		"idempotency_key",
		"request_hash",
		"response",
		"created_at",
		"expires_at",
	})

	assertPrimaryKey(t, db, "processed_events", []string{"consumer_name", "event_id"})
	assertPrimaryKey(t, db, "idempotency_requests", []string{"actor_id", "operation", "idempotency_key"})
	assertIndex(t, db, "submissions", "idx_submissions_status_judge_deadline_at_id")
	assertIndex(t, db, "outbox_events", "idx_outbox_events_status_next_retry_at_id")
	assertIndex(t, db, "idempotency_requests", "idx_idempotency_requests_expires_at")

	assertSubmissionConstraints(t, db)
}

func assertSubmissionConstraints(t *testing.T, db *sql.DB) {
	t.Helper()
	result, err := db.ExecContext(t.Context(), `
		INSERT INTO submissions (
			user_id, problem_id, language,
			source_object_key, source_sha256, source_size_bytes,
			judge_revision, status, retry_count, judge_deadline_at,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, UTC_TIMESTAMP(3) + INTERVAL 10 MINUTE, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3))
	`, 1001, 2001, "cpp", "sources/test/source.cpp",
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		24, "01K5C6Y7N8P9Q0R1S2T3V4W5X6", "QUEUED", 0)
	if err != nil {
		t.Fatalf("insert valid submission: %v", err)
	}
	submissionID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM outbox_events WHERE aggregate_type = 'submission' AND aggregate_id = ?`, submissionID)
		_, _ = db.Exec(`DELETE FROM submissions WHERE id = ?`, submissionID)
	})

	_, err = db.ExecContext(t.Context(), `UPDATE submissions SET retry_count = 4 WHERE id = ?`, submissionID)
	assertMySQLConstraintError(t, err, "retry_count above maximum")

	_, err = db.ExecContext(t.Context(), `
		INSERT INTO outbox_events (
			event_id, aggregate_type, aggregate_id, event_type, event_version,
			payload, status, retry_count, next_retry_at, lease_owner,
			created_at
		) VALUES (?, 'submission', ?, 'judge.requested', 1, JSON_OBJECT(), 'pending', 0, UTC_TIMESTAMP(3), 'relay-1', UTC_TIMESTAMP(3))
	`, "550e8400-e29b-41d4-a716-446655440000", submissionID)
	assertMySQLConstraintError(t, err, "partial outbox lease")
}

func assertMySQLConstraintError(t *testing.T, err error, operation string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s unexpectedly succeeded", operation)
	}
	if errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("%s returned unrelated error: %v", operation, err)
	}
}

func assertColumns(t *testing.T, db *sql.DB, table string, columns []string) {
	t.Helper()
	for _, column := range columns {
		var count int
		err := db.QueryRowContext(t.Context(), `
			SELECT COUNT(*)
			FROM information_schema.columns
			WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?
		`, table, column).Scan(&count)
		if err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("column %s.%s count = %d, want 1", table, column, count)
		}
	}
}

func assertMissingColumns(t *testing.T, db *sql.DB, table string, columns []string) {
	t.Helper()
	for _, column := range columns {
		var count int
		err := db.QueryRowContext(t.Context(), `
			SELECT COUNT(*)
			FROM information_schema.columns
			WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?
		`, table, column).Scan(&count)
		if err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("legacy column %s.%s still exists", table, column)
		}
	}
}

func assertPrimaryKey(t *testing.T, db *sql.DB, table string, want []string) {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), `
		SELECT column_name
		FROM information_schema.key_column_usage
		WHERE table_schema = DATABASE() AND table_name = ? AND constraint_name = 'PRIMARY'
		ORDER BY ordinal_position
	`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var got []string
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			t.Fatal(err)
		}
		got = append(got, column)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("primary key for %s = %v, want %v", table, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("primary key for %s = %v, want %v", table, got, want)
		}
	}
}

func assertIndex(t *testing.T, db *sql.DB, table, index string) {
	t.Helper()
	var count int
	err := db.QueryRowContext(t.Context(), `
		SELECT COUNT(*)
		FROM information_schema.statistics
		WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?
	`, table, index).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatalf("index %s.%s does not exist", table, index)
	}
}

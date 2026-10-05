package data

import (
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	mysql "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/viggggil/go_oj_agent/services/judge/internal/biz"
)

func TestJudgeContestFinalInsertUsesDatabaseBoundary(t *testing.T) {
	dsn := os.Getenv("SUBMISSION_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set SUBMISSION_TEST_MYSQL_DSN")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := "judge_deadline_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec("CREATE DATABASE " + schema); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec("DROP DATABASE " + schema)
	cfg.DBName = schema
	cfg.MultiStatements = true
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, source, _, _ := runtime.Caller(0)
	files, err := filepath.Glob(filepath.Join(filepath.Dir(source), "../../../../migrations/submission/*.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(strings.ReplaceAll(string(body), "USE oj_submission;", "USE "+schema+";")); err != nil {
			t.Fatalf("migration %s: %v", file, err)
		}

	}
	now := time.Unix(1791000000, 500000000).UTC()
	for _, tc := range []struct {
		name       string
		start, end time.Time
		allowed    bool
	}{
		{"at start", now, now.Add(time.Hour), true},
		{"before start", now.Add(time.Millisecond), now.Add(time.Hour), false},
		{"at end", now.Add(-time.Hour), now, false},
		{"after end", now.Add(-time.Hour), now.Add(-time.Millisecond), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn, err := db.Conn(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			// 固定本测试连接的 MySQL statement clock，精确验证相等边界。
			if _, err := conn.ExecContext(t.Context(), "SET timestamp = 1791000000.5"); err != nil {
				t.Fatal(err)
			}
			defer conn.ExecContext(t.Context(), "SET timestamp = 0")
			tx, err := conn.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			sub := createCommand(now).Submission
			sub.ContestID = 20
			id, err := insertContestSubmission(t.Context(), tx, &sub, tc.start, tc.end)
			if tc.allowed {
				if err != nil || id <= 0 || !sub.CreatedAt.Equal(now) {
					t.Fatalf("insert=%d created=%v err=%v", id, sub.CreatedAt, err)
				}
			} else if !biz.HasReason(err, biz.ReasonInvalidTransition) {
				t.Fatalf("outside interval insert=%d err=%v", id, err)
			}
		})
	}
}

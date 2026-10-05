package data

import (
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	mysql "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

func testContestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("CONTEST_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set CONTEST_TEST_MYSQL_DSN (permission to create temporary schema required)")
	}
	admin, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "contest_cache_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec("CREATE DATABASE " + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP DATABASE " + schema); err != nil {
			t.Error(err)
		}
	})
	cfg.DBName = schema
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	_, source, _, _ := runtime.Caller(0)
	files, err := filepath.Glob(filepath.Join(filepath.Dir(source), "../../../../migrations/contest/*.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, statement := range strings.Split(strings.ReplaceAll(string(body), "USE oj_contest;", "USE "+schema+";"), ";") {
			if strings.TrimSpace(statement) == "" {
				continue
			}
			if _, err := db.Exec(statement); err != nil {
				t.Fatalf("migration %s: %v", file, err)
			}
		}
	}
	return db
}

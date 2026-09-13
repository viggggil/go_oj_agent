package data

import (
	"context"
	"github.com/DATA-DOG/go-sqlmock"
	"testing"
	"time"
)

func TestArchiveProblemRepository(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now()
	mock.ExpectExec("UPDATE problems SET status").WithArgs("PROBLEM_STATUS_ARCHIVED", int64(3), "PROBLEM_STATUS_NORMAL").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT id, title, slug, description").WithArgs(int64(3)).WillReturnRows(sqlmock.NewRows([]string{"id", "title", "slug", "description", "difficulty", "time_limit_ms", "memory_limit_kb", "status", "created_by", "created_at", "updated_at"}).AddRow(3, "A+B", "a-plus-b", "S", "PROBLEM_DIFFICULTY_EASY", 1000, 65536, "PROBLEM_STATUS_ARCHIVED", 1, now, now))
	mock.ExpectQuery("SELECT t.id, t.name").WithArgs(int64(3)).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}))
	got, err := NewStoreSet(db).Archive(context.Background(), 3)
	if err != nil || got.ID != 3 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

package data

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestRepositoryJoinIsIdempotent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	joinedAt := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	startAt := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	endAt := startAt.Add(time.Hour)
	for range 2 {
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT start_at,end_at,status,UTC_TIMESTAMP\\(3\\) FROM contests WHERE id=\\? FOR UPDATE").
			WithArgs(int64(20)).
			WillReturnRows(sqlmock.NewRows([]string{"start_at", "end_at", "status", "UTC_TIMESTAMP(3)"}).AddRow(startAt, endAt, "draft", time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)))
		mock.ExpectExec("INSERT IGNORE INTO contest_participants").
			WithArgs(int64(20), int64(42)).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery("SELECT joined_at FROM contest_participants").
			WithArgs(int64(20), int64(42)).
			WillReturnRows(sqlmock.NewRows([]string{"joined_at"}).AddRow(joinedAt))
		mock.ExpectCommit()
	}

	repository := NewRepository(db)
	for range 2 {
		got, err := repository.Join(context.Background(), 20, 42)
		if err != nil {
			t.Fatalf("Join() error = %v", err)
		}
		if !got.Equal(joinedAt) {
			t.Fatalf("Join() = %v, want %v", got, joinedAt)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

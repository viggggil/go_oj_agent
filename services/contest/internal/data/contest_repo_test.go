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
	for range 2 {
		mock.ExpectExec("INSERT IGNORE INTO contest_participants").
			WithArgs(int64(20), int64(42)).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery("SELECT joined_at FROM contest_participants").
			WithArgs(int64(20), int64(42)).
			WillReturnRows(sqlmock.NewRows([]string{"joined_at"}).AddRow(joinedAt))
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

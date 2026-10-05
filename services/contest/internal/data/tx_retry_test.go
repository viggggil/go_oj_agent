package data

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	mysql "github.com/go-sql-driver/mysql"
)

func TestWithContestTxRetriesDeadlockAndHonorsCancellation(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	deadlock := &mysql.MySQLError{Number: 1213, Message: "deadlock"}
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE contests").WillReturnError(deadlock)
	mock.ExpectRollback()
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE contests").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := withContestTx(context.Background(), db, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(context.Background(), "UPDATE contests SET title=?", "x")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := withContestTx(ctx, db, func(*sql.Tx) error { return errors.New("must not run") }); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

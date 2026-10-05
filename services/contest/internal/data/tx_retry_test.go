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

func TestWithContestTxBoundsLockTimeoutAttempts(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	timeout := &mysql.MySQLError{Number: 1205, Message: "lock timeout"}
	for range maxTransactionAttempts {
		mock.ExpectBegin()
		mock.ExpectExec("UPDATE contests").WillReturnError(timeout)
		mock.ExpectRollback()
	}
	attempts := 0
	err = withContestTx(t.Context(), db, func(tx *sql.Tx) error {
		attempts++
		_, err := tx.ExecContext(t.Context(), "UPDATE contests SET title=?", "x")
		return err
	})
	if !errors.Is(err, timeout) || attempts != maxTransactionAttempts {
		t.Fatalf("attempts=%d err=%v", attempts, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestWithContestTxCancellationInterruptsRetryBackoff(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE contests").WillReturnError(&mysql.MySQLError{Number: 1213})
	mock.ExpectRollback()
	err = withContestTx(ctx, db, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE contests SET title=?", "x")
		cancel()
		return err
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("backoff cancellation=%v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

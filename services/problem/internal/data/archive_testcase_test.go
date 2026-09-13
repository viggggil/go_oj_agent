package data

import (
	"context"
	"github.com/DATA-DOG/go-sqlmock"
	"testing"
	"time"
)

func TestArchiveTestcaseRepository(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now()
	mock.ExpectExec("UPDATE testcases SET status").WithArgs("TESTCASE_STATUS_ARCHIVED", int64(7), int64(2)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT id, problem_id, case_no").WithArgs(int64(2)).WillReturnRows(sqlmock.NewRows([]string{"id", "problem_id", "case_no", "input_object_key", "output_object_key", "input_sha256", "output_sha256", "input_size_bytes", "output_size_bytes", "status", "created_at", "archived_at"}).AddRow(7, 2, 1, "in", "out", "ih", "oh", 2, 3, "TESTCASE_STATUS_ARCHIVED", now, now))
	got, err := NewStoreSet(db).ArchiveTestcase(context.Background(), 2, 7)
	if err != nil || got.ID != 7 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

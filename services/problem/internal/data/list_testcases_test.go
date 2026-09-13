package data

import (
	"context"
	"github.com/DATA-DOG/go-sqlmock"
	"testing"
	"time"
)

func TestListTestcasesRepositoryFiltersActive(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT id, problem_id, case_no").WithArgs(int64(2), "TESTCASE_STATUS_ACTIVE").WillReturnRows(sqlmock.NewRows([]string{"id", "problem_id", "case_no", "input_object_key", "output_object_key", "input_sha256", "output_sha256", "input_size_bytes", "output_size_bytes", "status", "created_at", "archived_at"}).AddRow(1, 2, 1, "in", "out", "ih", "oh", 2, 3, "TESTCASE_STATUS_ACTIVE", time.Now(), nil))
	items, err := NewStoreSet(db).ListTestcases(context.Background(), 2, false)
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%v err=%v", items, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

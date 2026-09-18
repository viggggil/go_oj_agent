package data

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"github.com/viggggil/go_oj_agent/services/problem/internal/biz"
)

func TestScanArchivedTestcase(t *testing.T) {
	now := time.Now().UTC()
	rows := sqlmock.NewRows([]string{"id", "problem_id", "case_no", "input_object_key", "output_object_key", "input_sha256", "output_sha256", "input_size_bytes", "output_size_bytes", "status", "created_at", "archived_at"}).
		AddRow(7, 2, 1, "in", "out", "ih", "oh", 2, 3, "TESTCASE_STATUS_ARCHIVED", now, now)
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT testcase").WillReturnRows(rows)
	row := db.QueryRow("SELECT testcase")
	var testcase biz.Testcase
	if err := scanTestcase(row, &testcase); err != nil {
		t.Fatal(err)
	}
	if testcase.Status != problemv1.TestcaseStatus_TESTCASE_STATUS_ARCHIVED || testcase.ArchivedAt == nil {
		t.Fatalf("testcase = %+v", testcase)
	}
}

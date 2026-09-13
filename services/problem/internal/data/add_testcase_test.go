package data

import (
	"context"
	"github.com/DATA-DOG/go-sqlmock"
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"github.com/viggggil/go_oj_agent/services/problem/internal/biz"
	"testing"
	"time"
)

func TestAddTestcaseRepository(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectExec("INSERT INTO testcases").WithArgs(int64(2), int32(1), "problem-2/input/1.in", "problem-2/output/1.out", "ih", "oh", int64(2), int64(3), "TESTCASE_STATUS_ACTIVE").WillReturnResult(sqlmock.NewResult(8, 1))
	mock.ExpectQuery("SELECT created_at").WithArgs(int64(8)).WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(time.Now()))
	got, err := NewStoreSet(db).AddTestcase(context.Background(), biz.Testcase{ProblemID: 2, CaseNo: 1, InputObjectKey: "problem-2/input/1.in", OutputObjectKey: "problem-2/output/1.out", InputSHA256: "ih", OutputSHA256: "oh", InputSizeBytes: 2, OutputSizeBytes: 3, Status: problemv1.TestcaseStatus_TESTCASE_STATUS_ACTIVE})
	if err != nil || got.ID != 8 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

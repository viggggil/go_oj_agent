package data

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"github.com/viggggil/go_oj_agent/services/problem/internal/biz"
)

const testJudgeRevision = "01K5C6Y7N8P9Q0R1S2T3V4W5X6"

func TestCommitAddedTestcaseAtomicallyUpdatesLatestStateAndRevisionPointer(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().UTC()
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT status, active_judge_revision FROM problems WHERE id = ? FOR UPDATE")).WithArgs(int64(2)).
		WillReturnRows(sqlmock.NewRows([]string{"status", "active_judge_revision"}).AddRow("PROBLEM_STATUS_NORMAL", nil))
	mock.ExpectExec("INSERT INTO testcases").WillReturnResult(sqlmock.NewResult(7, 1))
	mock.ExpectExec("UPDATE problems SET active_judge_revision").WithArgs(testJudgeRevision, int64(2)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT created_at FROM testcases").WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(now))
	mock.ExpectCommit()

	testcase := biz.Testcase{ProblemID: 2, CaseNo: 1, InputObjectKey: "in", OutputObjectKey: "out", InputSHA256: "ih", OutputSHA256: "oh", InputSizeBytes: 2, OutputSizeBytes: 3, Status: problemv1.TestcaseStatus_TESTCASE_STATUS_ACTIVE}
	got, err := NewStoreSet(db).CommitAddedTestcase(context.Background(), testcase, testJudgeRevision, "")
	if err != nil || got.ID != 7 {
		t.Fatalf("CommitAddedTestcase() = %+v, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCommitAddedTestcaseRollsBackWhenRevisionPointerChanged(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT status, active_judge_revision").WithArgs(int64(2)).
		WillReturnRows(sqlmock.NewRows([]string{"status", "active_judge_revision"}).AddRow("PROBLEM_STATUS_NORMAL", "different-revision-value"))
	mock.ExpectRollback()

	_, err = NewStoreSet(db).CommitAddedTestcase(context.Background(), biz.Testcase{ProblemID: 2}, testJudgeRevision, "")
	if !problemv1.IsProblemErrorReasonInvalidStatus(err) {
		t.Fatalf("expected revision conflict, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCommitArchivedTestcaseAtomicallyUpdatesLatestStateAndRevisionPointer(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().UTC()
	oldRevision := "01K5C6Y7N8P9Q0R1S2T3V4W5X5"
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT status, active_judge_revision").WithArgs(int64(2)).
		WillReturnRows(sqlmock.NewRows([]string{"status", "active_judge_revision"}).AddRow("PROBLEM_STATUS_NORMAL", oldRevision))
	mock.ExpectExec("UPDATE testcases SET status").WithArgs("TESTCASE_STATUS_ARCHIVED", int64(7), int64(2), "TESTCASE_STATUS_ACTIVE").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE problems SET active_judge_revision").WithArgs(testJudgeRevision, int64(2)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT id, problem_id, case_no").WithArgs(int64(7), int64(2)).WillReturnRows(
		sqlmock.NewRows([]string{"id", "problem_id", "case_no", "input_object_key", "output_object_key", "input_sha256", "output_sha256", "input_size_bytes", "output_size_bytes", "status", "created_at", "archived_at"}).
			AddRow(7, 2, 1, "in", "out", "ih", "oh", 2, 3, "TESTCASE_STATUS_ARCHIVED", now, now),
	)
	mock.ExpectCommit()

	got, err := NewStoreSet(db).CommitArchivedTestcase(context.Background(), 2, 7, testJudgeRevision, oldRevision)
	if err != nil || got.Status != problemv1.TestcaseStatus_TESTCASE_STATUS_ARCHIVED {
		t.Fatalf("CommitArchivedTestcase() = %+v, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCommitArchivedLastTestcaseClearsActiveRevision(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().UTC()
	oldRevision := "01K5C6Y7N8P9Q0R1S2T3V4W5X6"
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT status, active_judge_revision").WithArgs(int64(2)).WillReturnRows(sqlmock.NewRows([]string{"status", "active_judge_revision"}).AddRow("PROBLEM_STATUS_NORMAL", oldRevision))
	mock.ExpectExec("UPDATE testcases SET status").WithArgs("TESTCASE_STATUS_ARCHIVED", int64(7), int64(2), "TESTCASE_STATUS_ACTIVE").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE problems SET active_judge_revision").WithArgs(nil, int64(2)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT id, problem_id, case_no").WithArgs(int64(7), int64(2)).WillReturnRows(sqlmock.NewRows([]string{"id", "problem_id", "case_no", "input_object_key", "output_object_key", "input_sha256", "output_sha256", "input_size_bytes", "output_size_bytes", "status", "created_at", "archived_at"}).AddRow(7, 2, 1, "in", "out", "ih", "oh", 2, 3, "TESTCASE_STATUS_ARCHIVED", now, now))
	mock.ExpectCommit()

	got, err := NewStoreSet(db).CommitArchivedTestcase(context.Background(), 2, 7, "", oldRevision)
	if err != nil || got.Status != problemv1.TestcaseStatus_TESTCASE_STATUS_ARCHIVED {
		t.Fatalf("CommitArchivedTestcase() = %+v, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

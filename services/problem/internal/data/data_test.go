package data

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	mysql "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"

	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"github.com/viggggil/go_oj_agent/services/problem/internal/biz"
)

func TestTranslateTestcaseDuplicate(t *testing.T) {
	err := translateTestcaseMySQLError(&mysql.MySQLError{Number: 1062, Message: "duplicate case number"})
	if !problemv1.IsProblemErrorReasonTestcaseAlreadyExists(err) {
		t.Fatalf("expected testcase already exists, got %v", err)
	}
}
func TestArchiveProblemRepository(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now()
	mock.ExpectExec("UPDATE problems SET status").WithArgs("PROBLEM_STATUS_ARCHIVED", int64(3), "PROBLEM_STATUS_NORMAL").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT id, title, slug, description").WithArgs(int64(3)).WillReturnRows(sqlmock.NewRows([]string{"id", "title", "slug", "description", "difficulty", "time_limit_ms", "memory_limit_kb", "active_judge_revision", "status", "created_by", "created_at", "updated_at"}).AddRow(3, "A+B", "a-plus-b", "S", "PROBLEM_DIFFICULTY_EASY", 1000, 65536, nil, "PROBLEM_STATUS_ARCHIVED", 1, now, now))
	mock.ExpectQuery("SELECT t.id, t.name").WithArgs(int64(3)).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}))
	got, err := NewStoreSet(db).Archive(context.Background(), 3)
	if err != nil || got.ID != 3 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
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
func TestFindProblemByID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().UTC()
	mock.ExpectQuery("SELECT id, title, slug, description").WithArgs(int64(9)).WillReturnRows(
		sqlmock.NewRows([]string{"id", "title", "slug", "description", "difficulty", "time_limit_ms", "memory_limit_kb", "active_judge_revision", "status", "created_by", "created_at", "updated_at"}).
			AddRow(9, "A+B", "a-plus-b", "Statement", "PROBLEM_DIFFICULTY_EASY", 1000, 65536, "01K5C6Y7N8P9Q0R1S2T3V4W5X6", "PROBLEM_STATUS_NORMAL", 1, now, now),
	)
	mock.ExpectQuery("SELECT t.id, t.name").WithArgs(int64(9)).WillReturnRows(
		sqlmock.NewRows([]string{"id", "name"}).AddRow(2, "math"),
	)

	problem, err := NewStoreSet(db).FindByID(context.Background(), 9)
	if err != nil {
		t.Fatalf("FindByID() error = %v", err)
	}
	if problem.Difficulty != problemv1.ProblemDifficulty_PROBLEM_DIFFICULTY_EASY || problem.Status != problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL || len(problem.Tags) != 1 {
		t.Fatalf("FindByID() = %+v", problem)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFindProblemByIDNotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT id, title, slug, description").WithArgs(int64(404)).WillReturnRows(
		sqlmock.NewRows([]string{"id", "title", "slug", "description", "difficulty", "time_limit_ms", "memory_limit_kb", "active_judge_revision", "status", "created_by", "created_at", "updated_at"}),
	)
	_, err = NewStoreSet(db).FindByID(context.Background(), 404)
	if !problemv1.IsProblemErrorReasonNotFound(err) {
		t.Fatalf("expected not found, got %v", err)
	}
}

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
func TestListProblemsFiltersArchivedForRegularUsers(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT COUNT").WithArgs("PROBLEM_STATUS_NORMAL").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery("SELECT id, title, slug, difficulty, status").
		WithArgs("PROBLEM_STATUS_NORMAL", int32(20), int32(0)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "title", "slug", "difficulty", "status"}).
			AddRow(1, "A+B", "a-plus-b", "PROBLEM_DIFFICULTY_EASY", "PROBLEM_STATUS_NORMAL"))
	items, total, err := NewStoreSet(db).List(context.Background(), 1, 20, false)
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("List() = %v, %d, %v", items, total, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestListProblemsIncludesArchivedForAdmin(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT COUNT").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery("SELECT id, title, slug, difficulty, status").WithArgs(int32(10), int32(10)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "title", "slug", "difficulty", "status"}))
	_, _, err = NewStoreSet(db).List(context.Background(), 2, 10, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
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
func TestRedisProblemCacheRoundTrip(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()
	cache := &RedisProblemCache{client: client, namespace: "test", ttl: time.Hour}
	ctx := context.Background()
	if err := cache.Set(ctx, biz.Problem{ID: 3, Title: "A+B"}); err != nil {
		t.Fatal(err)
	}
	got, found, err := cache.Get(ctx, 3)
	if err != nil || !found || got.Title != "A+B" {
		t.Fatalf("got=%+v found=%v err=%v", got, found, err)
	}
	if err := cache.Delete(ctx, 3); err != nil {
		t.Fatal(err)
	}
	_, found, _ = cache.Get(ctx, 3)
	if found {
		t.Fatal("cache entry was not deleted")
	}
}
func TestTranslateMySQLErrorDuplicate(t *testing.T) {
	err := translateMySQLError(&mysql.MySQLError{Number: 1062, Message: "duplicate"})
	if !problemv1.IsProblemErrorReasonAlreadyExists(err) {
		t.Fatalf("expected already exists, got %v", err)
	}
}

func TestCreateProblemRepositoryCommitsProblemAndTags(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().UTC()
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO problems").WithArgs(
		"Two Sum", "two-sum", "Statement", "PROBLEM_DIFFICULTY_EASY", int32(1000), int32(65536), "PROBLEM_STATUS_NORMAL", int64(7),
	).WillReturnResult(sqlmock.NewResult(42, 1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, name FROM tags WHERE name = ?")).WithArgs("array").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(3, "array"))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO problem_tags (problem_id, tag_id) VALUES (?, ?)")).
		WithArgs(int64(42), int64(3)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT created_at, updated_at FROM problems WHERE id = ?")).WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"created_at", "updated_at"}).AddRow(now, now))
	mock.ExpectCommit()

	created, err := NewStoreSet(db).Create(context.Background(), repositoryProblem(), []string{"array"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.ID != 42 || len(created.Tags) != 1 || created.Tags[0].Name != "array" || created.CreatedAt.IsZero() {
		t.Fatalf("Create() = %+v", created)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateProblemRepositoryRollsBackOnDuplicateSlug(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO problems").WillReturnError(&mysql.MySQLError{Number: 1062, Message: "duplicate slug"})
	mock.ExpectRollback()

	_, err = NewStoreSet(db).Create(context.Background(), repositoryProblem(), nil)
	if !problemv1.IsProblemErrorReasonAlreadyExists(err) {
		t.Fatalf("expected already exists, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateProblemRepositoryRollsBackOnTagAssociationFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	want := errors.New("association failed")
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO problems").WillReturnResult(sqlmock.NewResult(42, 1))
	mock.ExpectQuery("SELECT id, name FROM tags").WithArgs("array").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(3, "array"))
	mock.ExpectExec("INSERT INTO problem_tags").WithArgs(int64(42), int64(3)).WillReturnError(want)
	mock.ExpectRollback()

	_, err = NewStoreSet(db).Create(context.Background(), repositoryProblem(), []string{"array"})
	if !errors.Is(err, want) {
		t.Fatalf("Create() error = %v, want %v", err, want)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func repositoryProblem() biz.Problem {
	return biz.Problem{Title: "Two Sum", Slug: "two-sum", Description: "Statement",
		Difficulty: problemv1.ProblemDifficulty_PROBLEM_DIFFICULTY_EASY, TimeLimitMs: 1000, MemoryLimitKb: 65536,
		Status: problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL, CreatedBy: 7}
}

func TestDeleteCreatedProblem(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectExec("DELETE FROM problems").WithArgs(int64(12)).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := NewStoreSet(db).DeleteCreatedProblem(context.Background(), 12); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestUpdateProblemRepositoryReplacesTags(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p := repositoryProblem()
	p.ID = 4
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE problems SET").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("DELETE FROM problem_tags").WithArgs(int64(4)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT id, name FROM tags").WithArgs("dp").WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(2, "dp"))
	mock.ExpectExec("INSERT INTO problem_tags").WithArgs(int64(4), int64(2)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT updated_at").WithArgs(int64(4)).WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(time.Now()))
	mock.ExpectCommit()
	updated, err := NewStoreSet(db).Update(context.Background(), p, []string{"dp"})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Tags) != 1 {
		t.Fatalf("tags = %v", updated.Tags)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

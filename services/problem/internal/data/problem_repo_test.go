package data

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	mysql "github.com/go-sql-driver/mysql"
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"github.com/viggggil/go_oj_agent/services/problem/internal/biz"
)

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

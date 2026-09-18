package data

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
)

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

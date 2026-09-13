package data

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

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

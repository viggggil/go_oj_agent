package data

import (
	"context"
	"github.com/DATA-DOG/go-sqlmock"
	"regexp"
	"testing"
)

func TestBatchGetFiltersArchivedAndOmitsMissingIDs(t *testing.T) {
	for _, archived := range []bool{false, true} {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		repo := &StoreSet{db: db}
		query := "SELECT id,title,slug,difficulty,status FROM problems WHERE id IN (?,?)"
		if !archived {
			query += " AND status = ?"
		}
		expectation := mock.ExpectQuery(regexp.QuoteMeta(query + " ORDER BY id"))
		if archived {
			expectation.WithArgs(int64(1), int64(999))
		} else {
			expectation.WithArgs(int64(1), int64(999), "PROBLEM_STATUS_NORMAL")
		}
		expectation.WillReturnRows(sqlmock.NewRows([]string{"id", "title", "slug", "difficulty", "status"}).AddRow(1, "Two Sum", "two-sum", "PROBLEM_DIFFICULTY_EASY", "PROBLEM_STATUS_NORMAL"))
		got, err := repo.BatchGet(context.Background(), []int64{1, 999}, archived)
		if err != nil || len(got) != 1 || got[0].Title != "Two Sum" {
			t.Fatalf("items=%v error=%v", got, err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
		db.Close()
	}
}

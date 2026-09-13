package data

import (
	"context"
	"github.com/DATA-DOG/go-sqlmock"
	"testing"
	"time"
)

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

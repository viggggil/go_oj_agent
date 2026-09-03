package data

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	mysql "github.com/go-sql-driver/mysql"

	"github.com/viggggil/go_oj_agent/services/user/internal/biz"
)

func TestCreateUserWritesUserAndDefaultRoleInTransaction(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO users").
		WithArgs("alice", "alice@example.com", "hash", biz.UserStatusActive).
		WillReturnResult(sqlmock.NewResult(1001, 1))
	mock.ExpectQuery("SELECT id FROM roles WHERE name").
		WithArgs(biz.RoleUser).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(10))
	mock.ExpectExec("INSERT INTO user_roles").
		WithArgs(int64(1001), int64(10)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	store := NewStoreSet(db)
	got, err := store.CreateUser(context.Background(), biz.User{
		Username:     "alice",
		Email:        "alice@example.com",
		PasswordHash: "hash",
		Status:       biz.UserStatusActive,
	}, biz.RoleUser)
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	if got.ID != 1001 {
		t.Fatalf("ID = %d, want 1001", got.ID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestCreateUserMapsDuplicateKey(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO users").
		WillReturnError(&mysql.MySQLError{Number: 1062, Message: "Duplicate entry"})
	mock.ExpectRollback()

	_, err = NewStoreSet(db).CreateUser(context.Background(), biz.User{}, biz.RoleUser)
	if err != biz.ErrUserAlreadyExists {
		t.Fatalf("CreateUser() error = %v, want ErrUserAlreadyExists", err)
	}
}

func TestFindByAccountLoadsRoles(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT u.id, u.username").
		WithArgs("ALICE@example.com", "ALICE@example.com").
		WillReturnRows(sqlmock.NewRows(
			[]string{"id", "username", "email", "password_hash", "status", "name"},
		).
			AddRow(1001, "alice", "alice@example.com", "hash", "active", "user").
			AddRow(1001, "alice", "alice@example.com", "hash", "active", "admin"))

	got, err := NewStoreSet(db).FindByAccount(context.Background(), "ALICE@example.com")
	if err != nil {
		t.Fatalf("FindByAccount() error = %v", err)
	}
	if len(got.Roles) != 2 || got.Roles[0] != biz.RoleUser || got.Roles[1] != biz.RoleAdmin {
		t.Fatalf("Roles = %#v, want [user admin]", got.Roles)
	}
}

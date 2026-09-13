package data

import (
	"testing"

	mysql "github.com/go-sql-driver/mysql"
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
)

func TestTranslateMySQLErrorDuplicate(t *testing.T) {
	err := translateMySQLError(&mysql.MySQLError{Number: 1062, Message: "duplicate"})
	if !problemv1.IsProblemErrorReasonAlreadyExists(err) {
		t.Fatalf("expected already exists, got %v", err)
	}
}

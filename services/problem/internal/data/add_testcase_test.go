package data

import (
	"testing"

	mysql "github.com/go-sql-driver/mysql"
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
)

func TestTranslateTestcaseDuplicate(t *testing.T) {
	err := translateTestcaseMySQLError(&mysql.MySQLError{Number: 1062, Message: "duplicate case number"})
	if !problemv1.IsProblemErrorReasonTestcaseAlreadyExists(err) {
		t.Fatalf("expected testcase already exists, got %v", err)
	}
}

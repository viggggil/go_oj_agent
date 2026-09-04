package main

import (
	"os"
	"testing"
)

func TestLoadBootstrapInput(t *testing.T) {
	t.Setenv(envBootstrapDSN, "user:pass@tcp(127.0.0.1:3306)/oj_user")
	t.Setenv(envBootstrapUsername, " Admin ")
	t.Setenv(envBootstrapEmail, " ADMIN@example.com ")
	t.Setenv(envBootstrapPassword, " correct1 ")

	got, err := loadBootstrapInput()
	if err != nil {
		t.Fatalf("loadBootstrapInput() error = %v", err)
	}
	if got.username != "Admin" || got.email != "ADMIN@example.com" || got.password != "correct1" {
		t.Fatalf("input = %#v, want trimmed values", got)
	}
}

func TestLoadBootstrapInputRequiresPassword(t *testing.T) {
	t.Setenv(envBootstrapDSN, "user:pass@tcp(127.0.0.1:3306)/oj_user")
	t.Setenv(envBootstrapUsername, "admin")
	t.Setenv(envBootstrapEmail, "admin@example.com")
	_ = os.Unsetenv(envBootstrapPassword)

	if _, err := loadBootstrapInput(); err == nil {
		t.Fatal("loadBootstrapInput() error = nil, want non-nil")
	}
}

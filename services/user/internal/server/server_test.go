package server

import (
	"testing"

	"github.com/viggggil/go_oj_agent/services/user/internal/biz"
	"github.com/viggggil/go_oj_agent/services/user/internal/conf"
	userservice "github.com/viggggil/go_oj_agent/services/user/internal/service"
)

func TestNewServer(t *testing.T) {
	service := userservice.NewUserService(biz.NewUserUsecase(biz.UserUsecaseOptions{}))

	srv, err := New(conf.DefaultConfig(), service)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if srv.userService != service {
		t.Fatal("New() did not keep the provided service")
	}
}

func TestNewServerRequiresUserService(t *testing.T) {
	if _, err := New(conf.DefaultConfig(), nil); err == nil {
		t.Fatal("New() error = nil, want non-nil")
	}
}

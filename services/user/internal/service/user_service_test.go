package service

import (
	"testing"

	"github.com/viggggil/go_oj_agent/services/user/internal/biz"
)

func TestNewUserService(t *testing.T) {
	usecase := biz.NewUserUsecase(biz.UserUsecaseOptions{})
	service := NewUserService(usecase)

	if service.uc != usecase {
		t.Fatal("UserService did not keep the provided usecase")
	}
}

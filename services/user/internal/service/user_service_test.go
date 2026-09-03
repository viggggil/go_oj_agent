package service

import (
	"context"
	"testing"

	userv1 "github.com/viggggil/go_oj_agent/api/user/v1"
	"github.com/viggggil/go_oj_agent/services/user/internal/biz"
)

func TestNewUserService(t *testing.T) {
	usecase := biz.NewUserUsecase(biz.UserUsecaseOptions{})
	service := NewUserService(usecase)

	if service.uc != usecase {
		t.Fatal("UserService did not keep the provided usecase")
	}
}

func TestRegisterMapsProtoRequestAndResponse(t *testing.T) {
	repository := &fakeUserRepository{}
	usecase := biz.NewUserUsecase(biz.UserUsecaseOptions{
		Users:     repository,
		Passwords: fakePasswordHasher{},
	})
	service := NewUserService(usecase)

	response, err := service.Register(context.Background(), &userv1.RegisterRequest{
		Username: " Alice ",
		Email:    "ALICE@example.com",
		Password: "correct1",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if response.GetUser().GetUsername() != "alice" {
		t.Fatalf("response username = %q, want %q", response.GetUser().GetUsername(), "alice")
	}
	if response.GetUser().GetEmail() != "alice@example.com" {
		t.Fatalf("response email = %q, want %q", response.GetUser().GetEmail(), "alice@example.com")
	}
	if repository.created.PasswordHash != "hashed-password" {
		t.Fatalf("stored password hash = %q, want %q", repository.created.PasswordHash, "hashed-password")
	}
}

type fakeUserRepository struct {
	created biz.User
}

func (r *fakeUserRepository) CreateUser(_ context.Context, user biz.User, _ biz.RoleName) (biz.User, error) {
	r.created = user
	user.ID = 1001
	return user, nil
}

func (r *fakeUserRepository) FindByID(_ context.Context, userID int64) (biz.User, error) {
	return biz.User{ID: userID}, nil
}

func (r *fakeUserRepository) FindByAccount(_ context.Context, account string) (biz.User, error) {
	return biz.User{Username: account}, nil
}

type fakePasswordHasher struct{}

func (fakePasswordHasher) Hash(string) (string, error) {
	return "hashed-password", nil
}

func (fakePasswordHasher) Compare(string, string) error {
	return nil
}

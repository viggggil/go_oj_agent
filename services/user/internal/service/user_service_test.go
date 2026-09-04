package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

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

func TestLoginMapsProtoRequestAndResponse(t *testing.T) {
	repository := &fakeUserRepository{
		account: biz.User{
			ID:           1001,
			Username:     "alice",
			Email:        "alice@example.com",
			PasswordHash: "hashed-password",
			Status:       biz.UserStatusActive,
			Roles:        []biz.RoleName{biz.RoleUser},
		},
	}
	issuer := &fakeTokenIssuer{
		accessToken: "access",
		expiresIn:   15 * time.Minute,
	}
	usecase := biz.NewUserUsecase(biz.UserUsecaseOptions{
		Users:         repository,
		Passwords:     fakePasswordHasher{},
		Tokens:        issuer,
		RefreshToken:  &fakeRefreshTokenManager{raw: "refresh"},
		RefreshTokens: &fakeRefreshTokenStore{},
	})
	service := NewUserService(usecase)

	response, err := service.Login(context.Background(), &userv1.LoginRequest{
		Account:  "ALICE@example.com",
		Password: "correct1",
	})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if response.GetAccessToken() != "access" || response.GetRefreshToken() != "refresh" {
		t.Fatalf("response tokens = %#v, want access/refresh", response)
	}
	if response.GetExpiresIn() != 900 {
		t.Fatalf("expires_in = %d, want 900", response.GetExpiresIn())
	}
}

func TestLoginMapsInvalidCredential(t *testing.T) {
	usecase := biz.NewUserUsecase(biz.UserUsecaseOptions{
		Users: &fakeUserRepository{
			account: biz.User{
				PasswordHash: "hashed-password",
				Status:       biz.UserStatusActive,
			},
		},
		Passwords:     fakePasswordHasher{compareErr: errors.New("mismatch")},
		Tokens:        &fakeTokenIssuer{},
		RefreshToken:  &fakeRefreshTokenManager{raw: "refresh"},
		RefreshTokens: &fakeRefreshTokenStore{},
	})

	_, err := NewUserService(usecase).Login(context.Background(), &userv1.LoginRequest{
		Account:  "alice",
		Password: "wrong",
	})
	if got := status.Code(err); got != codes.Unauthenticated {
		t.Fatalf("Login() status = %s, want %s", got, codes.Unauthenticated)
	}
}

func TestRefreshTokenMapsProtoRequestAndResponse(t *testing.T) {
	usecase := biz.NewUserUsecase(biz.UserUsecaseOptions{
		Users: &fakeUserRepository{
			account: biz.User{
				ID:       1001,
				Username: "alice",
				Status:   biz.UserStatusActive,
				Roles:    []biz.RoleName{biz.RoleUser},
			},
		},
		Tokens:       &fakeTokenIssuer{accessToken: "next-access", expiresIn: 15 * time.Minute},
		RefreshToken: &fakeRefreshTokenManager{raw: "next-refresh"},
		RefreshTokens: &fakeRefreshTokenStore{found: biz.RefreshTokenRecord{
			UserID:    1001,
			SessionID: "session-1",
			TokenID:   "token-1",
			TokenHash: "hash:old-refresh",
			ExpiresAt: time.Now().Add(time.Hour),
		}},
	})

	response, err := NewUserService(usecase).RefreshToken(context.Background(), &userv1.RefreshTokenRequest{
		RefreshToken: "old-refresh",
	})
	if err != nil {
		t.Fatalf("RefreshToken() error = %v", err)
	}
	if response.GetAccessToken() != "next-access" || response.GetRefreshToken() != "next-refresh" {
		t.Fatalf("response tokens = %#v, want next-access/next-refresh", response)
	}
}

type fakeUserRepository struct {
	created biz.User
	account biz.User
}

func (r *fakeUserRepository) CreateUser(_ context.Context, user biz.User, _ biz.RoleName) (biz.User, error) {
	r.created = user
	user.ID = 1001
	return user, nil
}

func (r *fakeUserRepository) FindByID(_ context.Context, userID int64) (biz.User, error) {
	if r.account.ID != 0 {
		return r.account, nil
	}
	return biz.User{ID: userID}, nil
}

func (r *fakeUserRepository) FindByAccount(_ context.Context, account string) (biz.User, error) {
	if r.account.ID == 0 && r.account.Username == "" && r.account.Email == "" {
		return biz.User{}, biz.ErrUserNotFound
	}
	return r.account, nil
}

type fakePasswordHasher struct {
	compareErr error
}

func (fakePasswordHasher) Hash(string) (string, error) {
	return "hashed-password", nil
}

func (h fakePasswordHasher) Compare(string, string) error {
	return h.compareErr
}

type fakeTokenIssuer struct {
	accessToken string
	expiresIn   time.Duration
}

func (i *fakeTokenIssuer) IssueAccessToken(_ context.Context, _ biz.User) (string, time.Duration, error) {
	return i.accessToken, i.expiresIn, nil
}

type fakeRefreshTokenManager struct {
	raw string
}

func (m *fakeRefreshTokenManager) Generate(
	userID int64,
	sessionID string,
	rotatedFrom string,
) (string, biz.RefreshTokenRecord, error) {
	return m.raw, biz.RefreshTokenRecord{
		UserID:      userID,
		SessionID:   sessionID,
		TokenID:     "next-token",
		TokenHash:   m.Hash(m.raw),
		ExpiresAt:   time.Now().Add(time.Hour),
		RotatedFrom: rotatedFrom,
	}, nil
}

func (*fakeRefreshTokenManager) Hash(token string) string {
	return "hash:" + token
}

type fakeRefreshTokenStore struct {
	found biz.RefreshTokenRecord
}

func (*fakeRefreshTokenStore) Save(context.Context, biz.RefreshTokenRecord) error {
	return nil
}

func (s *fakeRefreshTokenStore) FindByHash(_ context.Context, tokenHash string) (biz.RefreshTokenRecord, error) {
	if s.found.TokenHash != tokenHash {
		return biz.RefreshTokenRecord{}, biz.ErrRefreshTokenDenied
	}
	return s.found, nil
}

func (*fakeRefreshTokenStore) Rotate(context.Context, string, biz.RefreshTokenRecord) error {
	return nil
}

func (*fakeRefreshTokenStore) RevokeSession(context.Context, string) error {
	return nil
}

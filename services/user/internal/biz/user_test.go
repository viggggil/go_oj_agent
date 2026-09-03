package biz

import (
	"context"
	"errors"
	"testing"
)

func TestRegisterInputValidate(t *testing.T) {
	policy := DefaultPasswordPolicy()
	input := RegisterInput{
		Username: " alice ",
		Email:    " ALICE@example.com ",
		Password: "correct1",
	}

	if err := input.Validate(policy); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestRegisterInputNormalize(t *testing.T) {
	input := RegisterInput{
		Username: " alice ",
		Email:    " ALICE@example.com ",
		Password: "correct1",
	}

	got := input.Normalize()
	if got.Username != "alice" {
		t.Fatalf("Username = %q, want %q", got.Username, "alice")
	}
	if got.Email != "alice@example.com" {
		t.Fatalf("Email = %q, want %q", got.Email, "alice@example.com")
	}
	if got.Password != "correct1" {
		t.Fatalf("Password = %q, want %q", got.Password, "correct1")
	}
}

func TestRegisterInputNormalizesUsernameCase(t *testing.T) {
	input := RegisterInput{
		Username: " Alice ",
		Email:    "ALICE@example.com",
		Password: "correct1",
	}

	got := input.Normalize()
	if got.Username != "alice" {
		t.Fatalf("Username = %q, want %q", got.Username, "alice")
	}
}

func TestPasswordPolicyRejectsShortPassword(t *testing.T) {
	policy := DefaultPasswordPolicy()

	if err := policy.Validate("short"); err == nil {
		t.Fatal("Validate() error = nil, want non-nil")
	}
}

func TestPasswordPolicyRejectsBlankAfterTrim(t *testing.T) {
	policy := DefaultPasswordPolicy()

	if err := policy.Validate("        "); err == nil {
		t.Fatal("Validate() error = nil, want non-nil")
	}
}

func TestPasswordPolicyRejectsBcryptOverflow(t *testing.T) {
	policy := DefaultPasswordPolicy()
	password := "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstu"

	if len(password) <= BcryptMaxPasswordBytes {
		t.Fatalf("test password length = %d, want greater than %d", len(password), BcryptMaxPasswordBytes)
	}
	if err := policy.Validate(password); err == nil {
		t.Fatal("Validate() error = nil, want non-nil")
	}
}

func TestUserHasRole(t *testing.T) {
	user := User{
		ID:    1001,
		Roles: []RoleName{RoleUser, RoleAdmin},
	}

	if !user.HasRole(RoleAdmin) {
		t.Fatal("HasRole(RoleAdmin) = false, want true")
	}
	if user.HasRole(RoleName("unknown")) {
		t.Fatal("HasRole(unknown) = true, want false")
	}
}

func TestBcryptPasswordHasher(t *testing.T) {
	hasher := NewBcryptPasswordHasher(4)
	password := "correct1"

	hash, err := hasher.Hash(password)
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	if hash == password {
		t.Fatal("Hash() returned plaintext password")
	}
	if err := hasher.Compare(hash, password); err != nil {
		t.Fatalf("Compare() error = %v", err)
	}
	if err := hasher.Compare(hash, "wrong-password"); err == nil {
		t.Fatal("Compare() error = nil, want non-nil")
	}
}

func TestUserUsecaseRegister(t *testing.T) {
	repo := &fakeUserRepository{}
	hasher := &fakePasswordHasher{hash: "hashed-password"}
	uc := NewUserUsecase(UserUsecaseOptions{
		Users:     repo,
		Passwords: hasher,
	})

	got, err := uc.Register(context.Background(), RegisterInput{
		Username: " Alice ",
		Email:    "ALICE@example.com",
		Password: "correct1",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if got.Username != "alice" {
		t.Fatalf("Username = %q, want %q", got.Username, "alice")
	}
	if got.Email != "alice@example.com" {
		t.Fatalf("Email = %q, want %q", got.Email, "alice@example.com")
	}
	if repo.defaultRole != RoleUser {
		t.Fatalf("defaultRole = %q, want %q", repo.defaultRole, RoleUser)
	}
	if repo.created.PasswordHash != "hashed-password" {
		t.Fatalf("PasswordHash = %q, want %q", repo.created.PasswordHash, "hashed-password")
	}
	if repo.created.Status != UserStatusActive {
		t.Fatalf("Status = %q, want %q", repo.created.Status, UserStatusActive)
	}
}

type fakeUserRepository struct {
	created     User
	defaultRole RoleName
	account     User
}

func (r *fakeUserRepository) CreateUser(_ context.Context, user User, defaultRole RoleName) (User, error) {
	r.created = user
	r.defaultRole = defaultRole
	user.ID = 1001
	return user, nil
}

func (r *fakeUserRepository) FindByID(_ context.Context, userID int64) (User, error) {
	return User{ID: userID}, nil
}

func (r *fakeUserRepository) FindByAccount(_ context.Context, account string) (User, error) {
	if r.account.ID == 0 && r.account.Username == "" && r.account.Email == "" {
		return User{}, ErrUserNotFound
	}
	return r.account, nil
}

type fakePasswordHasher struct {
	hash       string
	err        error
	compareErr error
}

func (h *fakePasswordHasher) Hash(string) (string, error) {
	if h.err != nil {
		return "", h.err
	}
	return h.hash, nil
}

func (h *fakePasswordHasher) Compare(string, string) error {
	if h.compareErr != nil {
		return h.compareErr
	}
	return nil
}

type fakeTokenIssuer struct {
	pair TokenPair
	err  error
	user User
}

func (i *fakeTokenIssuer) Issue(_ context.Context, user User) (TokenPair, error) {
	i.user = user
	return i.pair, i.err
}

func TestUserUsecaseLogin(t *testing.T) {
	repo := &fakeUserRepository{}
	issuer := &fakeTokenIssuer{
		pair: TokenPair{
			AccessToken:  "access",
			RefreshToken: "refresh",
		},
	}
	uc := NewUserUsecase(UserUsecaseOptions{
		Users:     repo,
		Passwords: &fakePasswordHasher{hash: "hashed-password", compareErr: nil},
		Tokens:    issuer,
	})
	repo.account = User{
		ID:           1001,
		Username:     "alice",
		Email:        "alice@example.com",
		PasswordHash: "hashed-password",
		Status:       UserStatusActive,
		Roles:        []RoleName{RoleUser},
	}

	got, err := uc.Login(context.Background(), LoginInput{
		Account:  " ALICE@EXAMPLE.COM ",
		Password: "correct1",
	})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if got.AccessToken != "access" || got.RefreshToken != "refresh" {
		t.Fatalf("TokenPair = %#v, want access/refresh", got)
	}
	if issuer.user.ID != 1001 || !issuer.user.HasRole(RoleUser) {
		t.Fatalf("issued user = %#v, want loaded active user", issuer.user)
	}
}

func TestUserUsecaseLoginRejectsInvalidCredential(t *testing.T) {
	repo := &fakeUserRepository{
		account: User{
			PasswordHash: "hashed-password",
			Status:       UserStatusActive,
		},
	}
	uc := NewUserUsecase(UserUsecaseOptions{
		Users:     repo,
		Passwords: &fakePasswordHasher{compareErr: errors.New("mismatch")},
		Tokens:    &fakeTokenIssuer{},
	})

	if _, err := uc.Login(context.Background(), LoginInput{
		Account:  "alice",
		Password: "wrong",
	}); err != ErrInvalidCredential {
		t.Fatalf("Login() error = %v, want ErrInvalidCredential", err)
	}
}

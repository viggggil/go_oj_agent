package biz

import (
	"context"
	"errors"
	"testing"
	"time"
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
	if r.account.ID != 0 {
		return r.account, nil
	}
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
	accessToken string
	expiresIn   time.Duration
	err         error
	user        User
}

func (i *fakeTokenIssuer) IssueAccessToken(_ context.Context, user User) (string, time.Duration, error) {
	i.user = user
	return i.accessToken, i.expiresIn, i.err
}

type fakeRefreshTokenManager struct {
	raw    string
	record RefreshTokenRecord
}

func (m *fakeRefreshTokenManager) Generate(
	userID int64,
	sessionID string,
	rotatedFrom string,
) (string, RefreshTokenRecord, error) {
	record := m.record
	record.UserID = userID
	if record.SessionID == "" {
		record.SessionID = sessionID
	}
	if record.RotatedFrom == "" {
		record.RotatedFrom = rotatedFrom
	}
	if record.TokenHash == "" {
		record.TokenHash = m.Hash(m.raw)
	}
	return m.raw, record, nil
}

func (*fakeRefreshTokenManager) Hash(token string) string {
	return "hash:" + token
}

type fakeRefreshTokenStore struct {
	saved          RefreshTokenRecord
	found          RefreshTokenRecord
	findErr        error
	rotatedOldHash string
	rotatedNext    RefreshTokenRecord
}

func (s *fakeRefreshTokenStore) Save(_ context.Context, record RefreshTokenRecord) error {
	s.saved = record
	return nil
}

func (s *fakeRefreshTokenStore) FindByHash(_ context.Context, tokenHash string) (RefreshTokenRecord, error) {
	if s.findErr != nil {
		return RefreshTokenRecord{}, s.findErr
	}
	if s.found.TokenHash != tokenHash {
		return RefreshTokenRecord{}, ErrRefreshTokenDenied
	}
	return s.found, nil
}

func (s *fakeRefreshTokenStore) Rotate(_ context.Context, oldTokenHash string, next RefreshTokenRecord) error {
	s.rotatedOldHash = oldTokenHash
	s.rotatedNext = next
	return nil
}

func (*fakeRefreshTokenStore) RevokeSession(context.Context, string) error {
	return nil
}

func TestUserUsecaseLogin(t *testing.T) {
	repo := &fakeUserRepository{}
	issuer := &fakeTokenIssuer{
		accessToken: "access",
		expiresIn:   15 * time.Minute,
	}
	refreshStore := &fakeRefreshTokenStore{}
	uc := NewUserUsecase(UserUsecaseOptions{
		Users:         repo,
		Passwords:     &fakePasswordHasher{hash: "hashed-password", compareErr: nil},
		Tokens:        issuer,
		RefreshToken:  &fakeRefreshTokenManager{raw: "refresh"},
		RefreshTokens: refreshStore,
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
	if got.ExpiresIn != 15*time.Minute {
		t.Fatalf("ExpiresIn = %s, want 15m", got.ExpiresIn)
	}
	if refreshStore.saved.UserID != 1001 || refreshStore.saved.TokenHash != "hash:refresh" {
		t.Fatalf("saved refresh token = %#v, want user 1001 hash:refresh", refreshStore.saved)
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
		Users:         repo,
		Passwords:     &fakePasswordHasher{compareErr: errors.New("mismatch")},
		Tokens:        &fakeTokenIssuer{},
		RefreshToken:  &fakeRefreshTokenManager{raw: "refresh"},
		RefreshTokens: &fakeRefreshTokenStore{},
	})

	if _, err := uc.Login(context.Background(), LoginInput{
		Account:  "alice",
		Password: "wrong",
	}); err != ErrInvalidCredential {
		t.Fatalf("Login() error = %v, want ErrInvalidCredential", err)
	}
}

func TestUserUsecaseRefreshTokenRotatesToken(t *testing.T) {
	now := time.Now().UTC()
	repo := &fakeUserRepository{
		account: User{
			ID:       1001,
			Username: "alice",
			Status:   UserStatusActive,
			Roles:    []RoleName{RoleUser},
		},
	}
	refreshStore := &fakeRefreshTokenStore{
		found: RefreshTokenRecord{
			UserID:    1001,
			SessionID: "session-1",
			TokenID:   "token-1",
			TokenHash: "hash:old-refresh",
			ExpiresAt: now.Add(time.Hour),
		},
	}
	uc := NewUserUsecase(UserUsecaseOptions{
		Users:         repo,
		Passwords:     &fakePasswordHasher{},
		Tokens:        &fakeTokenIssuer{accessToken: "new-access", expiresIn: 15 * time.Minute},
		RefreshToken:  &fakeRefreshTokenManager{raw: "new-refresh"},
		RefreshTokens: refreshStore,
	})

	got, err := uc.RefreshToken(context.Background(), RefreshTokenInput{
		RefreshToken: "old-refresh",
	})
	if err != nil {
		t.Fatalf("RefreshToken() error = %v", err)
	}
	if got.AccessToken != "new-access" || got.RefreshToken != "new-refresh" {
		t.Fatalf("TokenPair = %#v, want new-access/new-refresh", got)
	}
	if refreshStore.rotatedOldHash != "hash:old-refresh" {
		t.Fatalf("rotated old hash = %q, want hash:old-refresh", refreshStore.rotatedOldHash)
	}
	if refreshStore.rotatedNext.SessionID != "session-1" || refreshStore.rotatedNext.RotatedFrom != "token-1" {
		t.Fatalf("rotated next = %#v, want same session and rotated token", refreshStore.rotatedNext)
	}
}

func TestUserUsecaseRefreshTokenRejectsRevokedToken(t *testing.T) {
	refreshStore := &fakeRefreshTokenStore{
		found: RefreshTokenRecord{
			UserID:    1001,
			TokenHash: "hash:old-refresh",
			ExpiresAt: time.Now().Add(time.Hour),
			Revoked:   true,
		},
	}
	uc := NewUserUsecase(UserUsecaseOptions{
		Users:         &fakeUserRepository{},
		Tokens:        &fakeTokenIssuer{},
		RefreshToken:  &fakeRefreshTokenManager{raw: "new-refresh"},
		RefreshTokens: refreshStore,
	})

	if _, err := uc.RefreshToken(context.Background(), RefreshTokenInput{
		RefreshToken: "old-refresh",
	}); err != ErrRefreshTokenDenied {
		t.Fatalf("RefreshToken() error = %v, want ErrRefreshTokenDenied", err)
	}
}

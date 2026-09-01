package biz

import "context"

type UserRepository interface {
	CreateUser(ctx context.Context, user User, defaultRole RoleName) (User, error)
	FindByID(ctx context.Context, userID int64) (User, error)
	FindByAccount(ctx context.Context, account string) (User, error)
}

type RoleRepository interface {
	ListUserRoles(ctx context.Context, userID int64) ([]RoleName, error)
}

type PasswordHasher interface {
	Hash(password string) (string, error)
	Compare(hash string, password string) error
}

type TokenIssuer interface {
	Issue(ctx context.Context, user User) (TokenPair, error)
}

type RefreshTokenStore interface {
	Save(ctx context.Context, record RefreshTokenRecord) error
	Rotate(ctx context.Context, oldToken string, next RefreshTokenRecord) error
	RevokeSession(ctx context.Context, sessionID string) error
}

type UserUsecase struct {
	users          UserRepository
	roles          RoleRepository
	passwords      PasswordHasher
	tokens         TokenIssuer
	refreshTokens  RefreshTokenStore
	passwordPolicy PasswordPolicy
}

type UserUsecaseOptions struct {
	Users          UserRepository
	Roles          RoleRepository
	Passwords      PasswordHasher
	Tokens         TokenIssuer
	RefreshTokens  RefreshTokenStore
	PasswordPolicy PasswordPolicy
}

func NewUserUsecase(options UserUsecaseOptions) *UserUsecase {
	policy := options.PasswordPolicy
	if policy.MinLength == 0 || policy.MaxBytes == 0 {
		policy = DefaultPasswordPolicy()
	}

	return &UserUsecase{
		users:          options.Users,
		roles:          options.Roles,
		passwords:      options.Passwords,
		tokens:         options.Tokens,
		refreshTokens:  options.RefreshTokens,
		passwordPolicy: policy,
	}
}

func (uc *UserUsecase) Register(ctx context.Context, input RegisterInput) (User, error) {
	if uc == nil || uc.users == nil || uc.passwords == nil {
		return User{}, ErrInvalidArgument
	}
	if err := input.Validate(uc.passwordPolicy); err != nil {
		return User{}, err
	}

	input = input.Normalize()
	passwordHash, err := uc.passwords.Hash(input.Password)
	if err != nil {
		return User{}, err
	}

	user := User{
		Username:     input.Username,
		Email:        input.Email,
		PasswordHash: passwordHash,
		Status:       UserStatusActive,
		Roles:        []RoleName{RoleUser},
	}

	return uc.users.CreateUser(ctx, user, RoleUser)
}

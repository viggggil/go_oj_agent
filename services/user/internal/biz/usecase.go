package biz

import (
	"context"
	"errors"
	"time"
)

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
	IssueAccessToken(ctx context.Context, user User) (string, time.Duration, error)
}

type RefreshTokenGenerator interface {
	Generate(userID int64, sessionID string, rotatedFrom string) (string, RefreshTokenRecord, error)
	Hash(token string) string
}

type RefreshTokenStore interface {
	Save(ctx context.Context, record RefreshTokenRecord) error
	FindByHash(ctx context.Context, tokenHash string) (RefreshTokenRecord, error)
	Rotate(ctx context.Context, oldTokenHash string, next RefreshTokenRecord) error
	RevokeSession(ctx context.Context, sessionID string) error
}

type UserUsecase struct {
	users          UserRepository
	roles          RoleRepository
	passwords      PasswordHasher
	tokens         TokenIssuer
	refreshToken   RefreshTokenGenerator
	refreshTokens  RefreshTokenStore
	passwordPolicy PasswordPolicy
}

type UserUsecaseOptions struct {
	Users          UserRepository
	Roles          RoleRepository
	Passwords      PasswordHasher
	Tokens         TokenIssuer
	RefreshToken   RefreshTokenGenerator
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
		refreshToken:   options.RefreshToken,
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

func (uc *UserUsecase) Login(ctx context.Context, input LoginInput) (TokenPair, error) {
	if uc == nil || uc.users == nil || uc.passwords == nil || uc.tokens == nil ||
		uc.refreshToken == nil || uc.refreshTokens == nil {
		return TokenPair{}, ErrInvalidArgument
	}
	if err := input.Validate(); err != nil {
		return TokenPair{}, err
	}

	input = input.Normalize()
	user, err := uc.users.FindByAccount(ctx, input.Account)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return TokenPair{}, ErrInvalidCredential
		}
		return TokenPair{}, err
	}
	if err := uc.passwords.Compare(user.PasswordHash, input.Password); err != nil {
		return TokenPair{}, ErrInvalidCredential
	}
	if !user.IsActive() {
		return TokenPair{}, ErrUserInactive
	}

	if len(user.Roles) == 0 && uc.roles != nil {
		user.Roles, err = uc.roles.ListUserRoles(ctx, user.ID)
		if err != nil {
			return TokenPair{}, err
		}
	}
	return uc.issueTokenPair(ctx, user, "", "")
}

func (uc *UserUsecase) RefreshToken(ctx context.Context, input RefreshTokenInput) (TokenPair, error) {
	if uc == nil || uc.users == nil || uc.tokens == nil ||
		uc.refreshToken == nil || uc.refreshTokens == nil {
		return TokenPair{}, ErrInvalidArgument
	}
	if err := input.Validate(); err != nil {
		return TokenPair{}, err
	}

	// 刷新时用客户端 token 计算 hash 后查找记录，避免服务端存储或查询 refresh token 原文。
	tokenHash := uc.refreshToken.Hash(input.RefreshToken)
	record, err := uc.refreshTokens.FindByHash(ctx, tokenHash)
	if err != nil {
		return TokenPair{}, ErrRefreshTokenDenied
	}
	now := time.Now()
	if record.Revoked || record.ReplayLocked || !record.ExpiresAt.After(now) {
		return TokenPair{}, ErrRefreshTokenDenied
	}

	user, err := uc.users.FindByID(ctx, record.UserID)
	if err != nil {
		return TokenPair{}, err
	}
	if !user.IsActive() {
		return TokenPair{}, ErrUserInactive
	}
	if len(user.Roles) == 0 && uc.roles != nil {
		user.Roles, err = uc.roles.ListUserRoles(ctx, user.ID)
		if err != nil {
			return TokenPair{}, err
		}
	}

	accessToken, expiresIn, err := uc.tokens.IssueAccessToken(ctx, user)
	if err != nil {
		return TokenPair{}, err
	}
	// 成功刷新必须轮换 refresh token：新 token 继承同一个 session，旧 token 由 store 标记撤销。
	nextRaw, nextRecord, err := uc.refreshToken.Generate(user.ID, record.SessionID, record.TokenID)
	if err != nil {
		return TokenPair{}, err
	}
	if err := uc.refreshTokens.Rotate(ctx, record.TokenHash, nextRecord); err != nil {
		return TokenPair{}, err
	}

	return TokenPair{
		AccessToken:  accessToken,
		RefreshToken: nextRaw,
		ExpiresIn:    expiresIn,
	}, nil
}

func (uc *UserUsecase) issueTokenPair(
	ctx context.Context,
	user User,
	sessionID string,
	rotatedFrom string,
) (TokenPair, error) {
	accessToken, expiresIn, err := uc.tokens.IssueAccessToken(ctx, user)
	if err != nil {
		return TokenPair{}, err
	}
	refreshToken, record, err := uc.refreshToken.Generate(user.ID, sessionID, rotatedFrom)
	if err != nil {
		return TokenPair{}, err
	}
	if err := uc.refreshTokens.Save(ctx, record); err != nil {
		return TokenPair{}, err
	}
	return TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    expiresIn,
	}, nil
}

package biz

import (
	"strings"
	"time"
	"unicode/utf8"
)

const BcryptMaxPasswordBytes = 72

type PasswordPolicy struct {
	MinLength int
	MaxBytes  int
}

func DefaultPasswordPolicy() PasswordPolicy {
	return PasswordPolicy{
		MinLength: 8,
		MaxBytes:  BcryptMaxPasswordBytes,
	}
}

func (p PasswordPolicy) Validate(password string) error {
	password = strings.TrimSpace(password)
	if password == "" {
		return ErrInvalidArgument
	}
	if utf8.RuneCountInString(password) < p.MinLength {
		return ErrInvalidArgument
	}
	if len(password) > p.MaxBytes {
		return ErrInvalidArgument
	}
	return nil
}

type TokenPair struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    time.Duration
}

type RefreshTokenRecord struct {
	UserID       int64
	SessionID    string
	TokenID      string
	TokenHash    string
	CreatedAt    time.Time
	ExpiresAt    time.Time
	LastUsedAt   time.Time
	RotatedFrom  string
	Revoked      bool
	ReplayLocked bool
}

func IsSupportedRole(role RoleName) bool {
	switch role {
	case RoleUser, RoleAdmin:
		return true
	default:
		return false
	}
}

func IsSupportedUserStatus(status UserStatus) bool {
	switch status {
	case UserStatusActive, UserStatusDisabled, UserStatusLocked:
		return true
	default:
		return false
	}
}

func IsBlank(value string) bool {
	return strings.TrimSpace(value) == ""
}

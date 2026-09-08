package biz

import (
	"strings"
)

type RegisterInput struct {
	Username string
	Email    string
	Password string
}

func (in RegisterInput) Normalize() RegisterInput {
	in.Username = NormalizeUsername(in.Username)
	in.Email = NormalizeEmail(in.Email)
	in.Password = strings.TrimSpace(in.Password)
	return in
}

type LoginInput struct {
	Account  string
	Password string
}

func (in LoginInput) Normalize() LoginInput {
	in.Account = strings.TrimSpace(in.Account)
	return in
}

type RefreshTokenInput struct {
	RefreshToken string
}

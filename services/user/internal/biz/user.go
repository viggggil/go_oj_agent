package biz

import "strings"

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

func (in RegisterInput) Validate(policy PasswordPolicy) error {
	in = in.Normalize()
	if IsBlank(in.Username) || IsBlank(in.Email) {
		return ErrInvalidArgument
	}
	if !strings.Contains(in.Email, "@") {
		return ErrInvalidArgument
	}
	return policy.Validate(in.Password)
}

type LoginInput struct {
	Account  string
	Password string
}

func (in LoginInput) Normalize() LoginInput {
	in.Account = strings.TrimSpace(in.Account)
	return in
}

func (in LoginInput) Validate() error {
	in = in.Normalize()
	if IsBlank(in.Account) || IsBlank(in.Password) {
		return ErrInvalidCredential
	}
	return nil
}

type RefreshTokenInput struct {
	RefreshToken string
}

func (in RefreshTokenInput) Validate() error {
	if IsBlank(in.RefreshToken) {
		return ErrRefreshTokenDenied
	}
	return nil
}

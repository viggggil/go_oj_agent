package security

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

const BcryptMaxPasswordBytes = 72

type PasswordPolicy struct {
	MinLength int
	MaxBytes  int
}

func DefaultPasswordPolicy() PasswordPolicy {
	return PasswordPolicy{MinLength: 8, MaxBytes: BcryptMaxPasswordBytes}
}

func (p PasswordPolicy) Validate(password string) error {
	password = strings.TrimSpace(password)
	if password == "" || utf8.RuneCountInString(password) < p.MinLength || len(password) > p.MaxBytes {
		return ErrInvalidArgument
	}
	return nil
}

type BcryptPasswordHasher struct {
	cost int
}

func NewBcryptPasswordHasher(cost int) BcryptPasswordHasher {
	if cost == 0 {
		cost = bcrypt.DefaultCost
	}
	return BcryptPasswordHasher{cost: cost}
}

func (h BcryptPasswordHasher) Hash(password string) (string, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), h.cost)
	if err != nil {
		return "", err
	}
	return string(hashed), nil
}

func (h BcryptPasswordHasher) Compare(hash string, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

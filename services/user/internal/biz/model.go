package biz

import "strings"

type RoleName string

const (
	RoleUser  RoleName = "user"
	RoleAdmin RoleName = "admin"
)

type UserStatus string

const (
	UserStatusActive   UserStatus = "active"
	UserStatusDisabled UserStatus = "disabled"
	UserStatusLocked   UserStatus = "locked"
)

type User struct {
	ID           int64
	Username     string
	Email        string
	PasswordHash string
	Status       UserStatus
	Roles        []RoleName
}

func (u User) IsActive() bool {
	return u.Status == UserStatusActive
}

func (u User) HasRole(role RoleName) bool {
	for _, current := range u.Roles {
		if current == role {
			return true
		}
	}
	return false
}

func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func NormalizeUsername(username string) string {
	return strings.TrimSpace(username)
}

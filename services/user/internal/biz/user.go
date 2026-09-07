package biz

import (
	"strings"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
)

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
	return strings.ToLower(strings.TrimSpace(username))
}

type RequestContext struct {
	UserID int64
	Roles  []RoleName
}

func NewRequestContext(ctx *commonv1.RequestContext) RequestContext {
	if ctx == nil {
		return RequestContext{}
	}
	roles := make([]RoleName, 0, len(ctx.GetRoles()))
	for _, role := range ctx.GetRoles() {
		roles = append(roles, RoleName(role))
	}
	return RequestContext{UserID: ctx.GetUserId(), Roles: roles}
}

func (c RequestContext) IsAdmin() bool {
	for _, role := range c.Roles {
		if role == RoleAdmin {
			return true
		}
	}
	return false
}

func IsBlank(value string) bool {
	return strings.TrimSpace(value) == ""
}

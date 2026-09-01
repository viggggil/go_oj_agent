package biz

import "errors"

var (
	ErrInvalidArgument    = errors.New("invalid argument")
	ErrInvalidCredential  = errors.New("invalid credential")
	ErrUserAlreadyExists  = errors.New("user already exists")
	ErrUserNotFound       = errors.New("user not found")
	ErrUserInactive       = errors.New("user inactive")
	ErrPermissionDenied   = errors.New("permission denied")
	ErrRefreshTokenDenied = errors.New("refresh token denied")
)

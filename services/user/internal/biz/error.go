package biz

import (
	kerrors "github.com/go-kratos/kratos/v3/errors"
	userv1 "github.com/viggggil/go_oj_agent/api/user/v1"
)

var (
	ErrInvalidArgument    = userv1.ErrorUserErrorReasonInvalidArgument("参数不合法")
	ErrInvalidCredential  = userv1.ErrorUserErrorReasonInvalidCredential("认证凭据无效")
	ErrUserAlreadyExists  = userv1.ErrorUserErrorReasonAlreadyExists("用户已存在")
	ErrUserNotFound       = userv1.ErrorUserErrorReasonNotFound("用户不存在")
	ErrAdminAlreadyExists = userv1.ErrorUserErrorReasonAdminAlreadyExists("管理员已存在")
	ErrUserInactive       = userv1.ErrorUserErrorReasonInactive("用户已禁用")
	ErrPermissionDenied   = userv1.ErrorUserErrorReasonPermissionDenied("权限不足")
	ErrRefreshTokenDenied = userv1.ErrorUserErrorReasonRefreshTokenDenied("刷新令牌无效")
)

func IsUserNotFound(err error) bool {
	return userv1.IsUserErrorReasonNotFound(err)
}

func IsAdminAlreadyExists(err error) bool {
	return userv1.IsUserErrorReasonAdminAlreadyExists(err)
}

func InvalidArgument(format string, args ...interface{}) *kerrors.Error {
	return userv1.ErrorUserErrorReasonInvalidArgument(format, args...)
}

func InvalidCredential(format string, args ...interface{}) *kerrors.Error {
	return userv1.ErrorUserErrorReasonInvalidCredential(format, args...)
}

func UserAlreadyExists(format string, args ...interface{}) *kerrors.Error {
	return userv1.ErrorUserErrorReasonAlreadyExists(format, args...)
}

func UserNotFound(format string, args ...interface{}) *kerrors.Error {
	return userv1.ErrorUserErrorReasonNotFound(format, args...)
}

func AdminAlreadyExists(format string, args ...interface{}) *kerrors.Error {
	return userv1.ErrorUserErrorReasonAdminAlreadyExists(format, args...)
}

func UserInactive(format string, args ...interface{}) *kerrors.Error {
	return userv1.ErrorUserErrorReasonInactive(format, args...)
}

func PermissionDenied(format string, args ...interface{}) *kerrors.Error {
	return userv1.ErrorUserErrorReasonPermissionDenied(format, args...)
}

func RefreshTokenDenied(format string, args ...interface{}) *kerrors.Error {
	return userv1.ErrorUserErrorReasonRefreshTokenDenied(format, args...)
}

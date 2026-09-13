package biz

import (
	kerrors "github.com/go-kratos/kratos/v3/errors"
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
)

func ErrorInvalidArgument(f string, a ...interface{}) *kerrors.Error {
	return problemv1.ErrorProblemErrorReasonInvalidArgument(f, a...)
}
func ErrorPermissionDenied(f string, a ...interface{}) *kerrors.Error {
	return problemv1.ErrorProblemErrorReasonPermissionDenied(f, a...)
}

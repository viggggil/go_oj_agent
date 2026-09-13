package biz

import (
	"fmt"

	kerrors "github.com/go-kratos/kratos/v3/errors"
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
)

func ErrorInvalidArgument(format string, args ...interface{}) *kerrors.Error {
	return problemv1.ErrorProblemErrorReasonInvalidArgument(format, args...)
}

func ErrorNotFound(format string, args ...interface{}) *kerrors.Error {
	return problemv1.ErrorProblemErrorReasonNotFound(format, args...)
}

func ErrorAlreadyExists(format string, args ...interface{}) *kerrors.Error {
	return problemv1.ErrorProblemErrorReasonAlreadyExists(format, args...)
}

func ErrorPermissionDenied(format string, args ...interface{}) *kerrors.Error {
	return problemv1.ErrorProblemErrorReasonPermissionDenied(format, args...)
}

func ErrorInvalidStatus(format string, args ...interface{}) *kerrors.Error {
	return problemv1.ErrorProblemErrorReasonInvalidStatus(format, args...)
}

func ErrorInternal(format string, args ...interface{}) *kerrors.Error {
	return kerrors.InternalServer("PROBLEM_INTERNAL", fmt.Sprintf(format, args...))
}

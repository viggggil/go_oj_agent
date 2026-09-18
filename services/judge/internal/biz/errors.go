package biz

import (
	"fmt"

	kerrors "github.com/go-kratos/kratos/v3/errors"
)

const (
	ReasonInvalidArgument       = "JUDGE_INVALID_ARGUMENT"
	ReasonUnauthenticated       = "JUDGE_UNAUTHENTICATED"
	ReasonPermissionDenied      = "JUDGE_PERMISSION_DENIED"
	ReasonSubmissionNotFound    = "SUBMISSION_NOT_FOUND"
	ReasonProblemUnavailable    = "PROBLEM_NOT_JUDGEABLE"
	ReasonInvalidTransition     = "SUBMISSION_INVALID_TRANSITION"
	ReasonIdempotencyConflict   = "IDEMPOTENCY_CONFLICT"
	ReasonIdempotencyInProgress = "IDEMPOTENCY_IN_PROGRESS"
	ReasonConcurrentRejudge     = "CONCURRENT_REJUDGE"
	ReasonDependencyUnavailable = "JUDGE_DEPENDENCY_UNAVAILABLE"
	ReasonInternal              = "JUDGE_INTERNAL"
)

func ErrorInvalidArgument(format string, args ...any) *kerrors.Error {
	return kerrors.BadRequest(ReasonInvalidArgument, fmt.Sprintf(format, args...))
}

func ErrorUnauthenticated(format string, args ...any) *kerrors.Error {
	return kerrors.Unauthorized(ReasonUnauthenticated, fmt.Sprintf(format, args...))
}

func ErrorPermissionDenied(format string, args ...any) *kerrors.Error {
	return kerrors.Forbidden(ReasonPermissionDenied, fmt.Sprintf(format, args...))
}

func ErrorSubmissionNotFound() *kerrors.Error {
	return kerrors.NotFound(ReasonSubmissionNotFound, "submission not found")
}

func ErrorProblemUnavailable(format string, args ...any) *kerrors.Error {
	return kerrors.New(412, ReasonProblemUnavailable, fmt.Sprintf(format, args...))
}

func ErrorInvalidTransition(format string, args ...any) *kerrors.Error {
	return kerrors.New(412, ReasonInvalidTransition, fmt.Sprintf(format, args...))
}

func ErrorIdempotencyConflict() *kerrors.Error {
	return kerrors.Conflict(ReasonIdempotencyConflict, "idempotency key was used for another request")
}

func ErrorIdempotencyInProgress() *kerrors.Error {
	return kerrors.Conflict(ReasonIdempotencyInProgress, "idempotent request is still processing")
}

func ErrorConcurrentRejudge() *kerrors.Error {
	return kerrors.Conflict(ReasonConcurrentRejudge, "submission was already rejudged")
}

func ErrorDependencyUnavailable(format string, args ...any) *kerrors.Error {
	return kerrors.ServiceUnavailable(ReasonDependencyUnavailable, fmt.Sprintf(format, args...))
}

func ErrorInternal(format string, args ...any) *kerrors.Error {
	return kerrors.InternalServer(ReasonInternal, fmt.Sprintf(format, args...))
}

func HasReason(err error, reason string) bool {
	return kerrors.Reason(err) == reason
}

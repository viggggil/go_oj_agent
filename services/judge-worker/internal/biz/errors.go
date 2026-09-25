package biz

import (
	"errors"
	"fmt"
	"strings"

	"github.com/viggggil/go_oj_agent/pkg/mq"
)

type SystemError struct {
	Reason    string
	Retryable bool
	Err       error
}

func (e *SystemError) Error() string {
	if e == nil {
		return "judge worker system error"
	}
	if e.Err == nil {
		return e.Reason
	}
	return fmt.Sprintf("%s: %v", e.Reason, e.Err)
}

func (e *SystemError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func NewSystemError(reason string, retryable bool, err error) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "WORKER_INTERNAL_ERROR"
	}
	return &SystemError{Reason: reason, Retryable: retryable, Err: err}
}

func ClassifySystemError(err error) (reason string, retryable bool) {
	var target *SystemError
	if errors.As(err, &target) {
		return target.Reason, target.Retryable
	}
	return "WORKER_INTERNAL_ERROR", true
}

// ClassifyFailure returns the stable protocol code and diagnostic message for
// an internal worker error. RabbitMQ policy must use Code/Retryable, never the
// diagnostic text.
func ClassifyFailure(err error) (mq.JudgeFailureCode, string, bool) {
	reason, retryable := ClassifySystemError(err)
	code := failureCodeForReason(reason)
	message := reason
	var target *SystemError
	if errors.As(err, &target) && target != nil && target.Err != nil {
		message = target.Error()
	}
	return code, message, retryable
}

func failureCodeForReason(reason string) mq.JudgeFailureCode {
	switch {
	case strings.Contains(reason, "SOURCE") || strings.Contains(reason, "INPUT_STORE"):
		if strings.Contains(reason, "UNAVAILABLE") || strings.Contains(reason, "STORE") {
			return mq.FailureSourceUnavailable
		}
		return mq.FailureSourceCorrupted
	case strings.Contains(reason, "REVISION") || strings.Contains(reason, "MANIFEST") || strings.Contains(reason, "SNAPSHOT") || strings.Contains(reason, "TESTCASE") || strings.Contains(reason, "INCOMPLETE"):
		return mq.FailureRevisionCorrupted
	case strings.Contains(reason, "DEADLINE") || strings.Contains(reason, "EXPIRED"):
		return mq.FailureTaskExpired
	case strings.Contains(reason, "SANDBOX") || strings.Contains(reason, "COMPILE_ARTIFACT"):
		if strings.Contains(reason, "UNAVAILABLE") {
			return mq.FailureSandboxUnavailable
		}
		return mq.FailureSandboxInternal
	default:
		return mq.FailureWorkerInternal
	}
}

package biz

import (
	"errors"
	"fmt"
	"strings"
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

package biz

import (
	"testing"

	userv1 "github.com/viggggil/go_oj_agent/api/user/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestProtoDefinedErrorCarriesGeneratedReasonAndCode(t *testing.T) {
	err := ErrUserNotFound
	if err.Reason != userv1.UserErrorReason_USER_ERROR_REASON_NOT_FOUND {
		t.Fatalf("reason = %s, want user not found", err.Reason)
	}
	if got := status.Code(err); got != codes.NotFound {
		t.Fatalf("status code = %s, want %s", got, codes.NotFound)
	}
}

func TestProtoDefinedErrorsMatchByReason(t *testing.T) {
	if !ErrUserNotFound.Is(NewError(userv1.UserErrorReason_USER_ERROR_REASON_NOT_FOUND)) {
		t.Fatal("errors with the same proto reason should match")
	}
	if ErrUserNotFound.Is(ErrUserAlreadyExists) {
		t.Fatal("errors with different proto reasons should not match")
	}
}

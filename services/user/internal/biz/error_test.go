package biz

import (
	"testing"

	kerrors "github.com/go-kratos/kratos/v3/errors"
	userv1 "github.com/viggggil/go_oj_agent/api/user/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestProtoDefinedErrorCarriesGeneratedReasonAndCode(t *testing.T) {
	err := ErrUserNotFound
	if got := kerrors.Reason(err); got != userv1.UserErrorReason_USER_ERROR_REASON_NOT_FOUND.String() {
		t.Fatalf("reason = %s, want user not found", got)
	}
	if got := kerrors.Code(err); got != 404 {
		t.Fatalf("http code = %d, want 404", got)
	}
	if got := status.Code(err); got != codes.NotFound {
		t.Fatalf("status code = %s, want %s", got, codes.NotFound)
	}
}

func TestProtoDefinedErrorsMatchByReason(t *testing.T) {
	if !userv1.IsUserErrorReasonNotFound(UserNotFound("用户不存在")) {
		t.Fatal("errors with the same proto reason should match")
	}
	if userv1.IsUserErrorReasonNotFound(ErrUserAlreadyExists) {
		t.Fatal("errors with different proto reasons should not match")
	}
}

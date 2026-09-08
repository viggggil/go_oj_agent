package biz

import (
	"errors"
	"fmt"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	userv1 "github.com/viggggil/go_oj_agent/api/user/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Error 是 user-service 的领域错误，原因和 gRPC code 均由 Proto 契约定义。
type Error struct {
	Reason userv1.UserErrorReason
	Cause  error
}

var (
	ErrInvalidArgument    = NewError(userv1.UserErrorReason_USER_ERROR_REASON_INVALID_ARGUMENT)
	ErrInvalidCredential  = NewError(userv1.UserErrorReason_USER_ERROR_REASON_INVALID_CREDENTIAL)
	ErrUserAlreadyExists  = NewError(userv1.UserErrorReason_USER_ERROR_REASON_ALREADY_EXISTS)
	ErrUserNotFound       = NewError(userv1.UserErrorReason_USER_ERROR_REASON_NOT_FOUND)
	ErrAdminAlreadyExists = NewError(userv1.UserErrorReason_USER_ERROR_REASON_ADMIN_ALREADY_EXISTS)
	ErrUserInactive       = NewError(userv1.UserErrorReason_USER_ERROR_REASON_INACTIVE)
	ErrPermissionDenied   = NewError(userv1.UserErrorReason_USER_ERROR_REASON_PERMISSION_DENIED)
	ErrRefreshTokenDenied = NewError(userv1.UserErrorReason_USER_ERROR_REASON_REFRESH_TOKEN_DENIED)
)

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause == nil {
		return e.Reason.String()
	}
	return fmt.Sprintf("%s: %v", e.Reason.String(), e.Cause)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func NewError(reason userv1.UserErrorReason) *Error {
	return &Error{Reason: reason}
}

func (e *Error) Is(target error) bool {
	var other *Error
	return errors.As(target, &other) && e != nil && other != nil && e.Reason == other.Reason
}

// GRPCStatus 使用 Proto enum option 中生成的 grpc_code，避免维护手写 reason/code 映射。
func (e *Error) GRPCStatus() *status.Status {
	code := codes.Internal
	if e != nil {
		if descriptor := userv1.File_api_user_v1_errors_proto.Enums().ByName("UserErrorReason"); descriptor != nil {
			value := descriptor.Values().ByNumber(protoreflect.EnumNumber(e.Reason))
			if value != nil {
				options := value.Options()
				if proto.HasExtension(options, commonv1.E_GrpcCode) {
					if grpcCode, ok := proto.GetExtension(options, commonv1.E_GrpcCode).(int32); ok {
						code = codes.Code(grpcCode)
					}
				}
			}
		}
	}
	message := e.Error()
	return status.New(code, message)
}

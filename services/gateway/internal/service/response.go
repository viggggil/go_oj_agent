package service

import (
	"encoding/json"
	"net/http"

	kerrors "github.com/go-kratos/kratos/v3/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Envelope struct {
	Data      any    `json:"data"`
	RequestID string `json:"request_id"`
}

type ErrorEnvelope struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

func WriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func WriteError(w http.ResponseWriter, err error, requestID string) {
	httpStatus, code, message := ErrorResponse(err)
	WriteJSON(w, httpStatus, ErrorEnvelope{
		Code:      code,
		Message:   message,
		RequestID: requestID,
	})
}

func ErrorResponse(err error) (int, string, string) {
	if err == nil {
		return http.StatusInternalServerError, "INTERNAL", "internal server error"
	}
	if ke := kerrors.FromError(err); ke.Reason != kerrors.UnknownReason {
		return int(ke.Code), ke.Reason, ke.Message
	}
	if st, ok := status.FromError(err); ok {
		return statusFromGRPCCode(st.Code()), st.Code().String(), st.Message()
	}
	return http.StatusInternalServerError, "INTERNAL", err.Error()
}

func statusFromGRPCCode(code codes.Code) int {
	switch code {
	case codes.OK:
		return http.StatusOK
	case codes.InvalidArgument:
		return http.StatusBadRequest
	case codes.Unauthenticated:
		return http.StatusUnauthorized
	case codes.PermissionDenied:
		return http.StatusForbidden
	case codes.NotFound:
		return http.StatusNotFound
	case codes.AlreadyExists, codes.Aborted:
		return http.StatusConflict
	case codes.ResourceExhausted:
		return http.StatusTooManyRequests
	case codes.FailedPrecondition:
		return http.StatusPreconditionFailed
	case codes.Unavailable:
		return http.StatusServiceUnavailable
	case codes.DeadlineExceeded:
		return http.StatusGatewayTimeout
	default:
		return http.StatusInternalServerError
	}
}

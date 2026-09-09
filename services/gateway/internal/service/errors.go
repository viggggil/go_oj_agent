package service

import (
	"fmt"

	kerrors "github.com/go-kratos/kratos/v3/errors"
)

const (
	reasonGatewayInvalidArgument  = "GATEWAY_INVALID_ARGUMENT"
	reasonGatewayMethodNotAllowed = "GATEWAY_METHOD_NOT_ALLOWED"
)

func ErrInvalidJSON(err error) *kerrors.Error {
	return kerrors.New(400, reasonGatewayInvalidArgument, fmt.Sprintf("invalid json request: %v", err))
}

func ErrInvalidRequest(err error) *kerrors.Error {
	return kerrors.New(400, reasonGatewayInvalidArgument, err.Error())
}

func ErrMethodNotAllowed(method string) *kerrors.Error {
	return kerrors.New(405, reasonGatewayMethodNotAllowed, fmt.Sprintf("method %s is not allowed", method))
}

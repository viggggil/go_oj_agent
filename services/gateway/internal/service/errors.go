package service

import kerrors "github.com/go-kratos/kratos/v3/errors"

const (
	reasonGatewayInvalidArgument = "GATEWAY_INVALID_ARGUMENT"
)

func ErrInvalidRequest(err error) *kerrors.Error {
	return kerrors.New(400, reasonGatewayInvalidArgument, err.Error())
}

package client

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/wire"
	"google.golang.org/grpc"

	kgrpc "github.com/go-kratos/kratos/v3/transport/grpc"

	userv1 "github.com/viggggil/go_oj_agent/api/user/v1"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/conf"
)

var ProviderSet = wire.NewSet()

type UserClient struct {
	userv1.UserServiceClient
	conn *grpc.ClientConn
}

func NewUserClient(ctx context.Context, config *conf.Bootstrap) (*UserClient, func(), error) {
	clientConfig := config.GetClients().GetUser()
	conn, cleanup, err := newGRPCConn(ctx, clientConfig)
	if err != nil {
		return nil, nil, err
	}
	return &UserClient{UserServiceClient: userv1.NewUserServiceClient(conn), conn: conn}, cleanup, nil
}

func newGRPCConn(ctx context.Context, config *conf.ClientProto) (*grpc.ClientConn, func(), error) {
	if config == nil || strings.TrimSpace(config.GetEndpoint()) == "" {
		return nil, nil, fmt.Errorf("gateway client endpoint is required")
	}
	timeout := 3 * time.Second
	if parsed, err := time.ParseDuration(config.GetTimeout()); err == nil && parsed > 0 {
		timeout = parsed
	}
	conn, err := kgrpc.NewClient(ctx, kgrpc.WithEndpoint(config.GetEndpoint()), kgrpc.WithTimeout(timeout))
	if err != nil {
		return nil, nil, err
	}
	return conn, func() { _ = conn.Close() }, nil
}

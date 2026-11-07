package client

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-kratos/kratos/v3/registry"
	"github.com/google/wire"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"

	kgrpc "github.com/go-kratos/kratos/v3/transport/grpc"

	userv1 "github.com/viggggil/go_oj_agent/api/user/v1"
	"github.com/viggggil/go_oj_agent/pkg/servicediscovery"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/conf"
)

var ProviderSet = wire.NewSet(NewClientContext, NewUserClient, NewProblemClient, NewSubmissionClient, NewContestClient, NewAgentClient, ProvideUserServiceClient, ProvideProblemServiceClient, ProvideSubmissionServiceClient, ProvideContestServiceClient)

type UserClient struct {
	userv1.UserServiceClient
	conn *grpc.ClientConn
}

func NewClientContext() context.Context {
	return context.Background()
}

func NewUserClient(ctx context.Context, config *conf.Bootstrap) (*UserClient, func(), error) {
	if config == nil || config.GetClients() == nil {
		return nil, nil, fmt.Errorf("gateway user client config is required")
	}
	clientConfig := config.GetClients().GetUser()
	discovery, err := newDiscovery(config)
	if err != nil {
		return nil, nil, err
	}
	conn, cleanup, err := newGRPCConnWithDiscovery(ctx, clientConfig, discovery)
	if err != nil {
		return nil, nil, err
	}
	return &UserClient{UserServiceClient: userv1.NewUserServiceClient(conn), conn: conn}, cleanup, nil
}

func ProvideUserServiceClient(client *UserClient) userv1.UserServiceClient {
	return client.UserServiceClient
}

func newGRPCConn(ctx context.Context, config *conf.ClientProto, interceptors ...grpc.UnaryClientInterceptor) (*grpc.ClientConn, func(), error) {
	return newGRPCConnWithDiscovery(ctx, config, nil, interceptors...)
}

func newGRPCConnWithDiscovery(ctx context.Context, config *conf.ClientProto, discovery registry.Discovery, interceptors ...grpc.UnaryClientInterceptor) (*grpc.ClientConn, func(), error) {
	if config == nil || strings.TrimSpace(config.GetEndpoint()) == "" {
		return nil, nil, fmt.Errorf("gateway client endpoint is required")
	}
	timeout := 3 * time.Second
	if parsed, err := time.ParseDuration(config.GetTimeout()); err == nil && parsed > 0 {
		timeout = parsed
	}
	endpoint := config.GetEndpoint()
	if discovery != nil {
		if strings.TrimSpace(config.GetName()) == "" {
			return nil, nil, fmt.Errorf("gateway client service name is required when Consul discovery is enabled")
		}
		endpoint = servicediscovery.Target(true, config.GetName(), endpoint)
	}
	options := []kgrpc.ClientOption{
		kgrpc.WithEndpoint(endpoint),
		kgrpc.WithTimeout(timeout),
		kgrpc.WithOptions(grpc.WithStatsHandler(otelgrpc.NewClientHandler())),
	}
	if discovery != nil {
		options = append(options, kgrpc.WithDiscovery(discovery))
	}
	if len(interceptors) > 0 {
		options = append(options, kgrpc.WithUnaryInterceptor(interceptors...))
	}
	conn, err := kgrpc.NewClient(ctx, options...)
	if err != nil {
		return nil, nil, err
	}
	return conn, func() { _ = conn.Close() }, nil
}

func newDiscovery(config *conf.Bootstrap) (registry.Discovery, error) {
	if config == nil || config.GetRegistry() == nil || config.GetRegistry().GetConsul() == nil {
		return nil, nil
	}
	c := config.GetRegistry().GetConsul()
	return servicediscovery.New(servicediscovery.Config{
		Enabled: c.GetEnabled(), Address: c.GetAddress(), Scheme: c.GetScheme(),
		Datacenter: c.GetDatacenter(), Token: c.GetToken(),
	})
}

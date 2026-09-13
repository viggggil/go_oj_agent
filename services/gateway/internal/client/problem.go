package client

import (
	"context"
	"fmt"
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/conf"
)

type ProblemClient struct{ problemv1.ProblemServiceClient }

func NewProblemClient(ctx context.Context, config *conf.Bootstrap) (*ProblemClient, func(), error) {
	if config == nil || config.GetClients() == nil {
		return nil, nil, fmt.Errorf("gateway problem client config is required")
	}
	conn, cleanup, err := newGRPCConn(ctx, config.GetClients().GetProblem())
	if err != nil {
		return nil, nil, err
	}
	return &ProblemClient{problemv1.NewProblemServiceClient(conn)}, cleanup, nil
}
func ProvideProblemServiceClient(c *ProblemClient) problemv1.ProblemServiceClient {
	return c.ProblemServiceClient
}

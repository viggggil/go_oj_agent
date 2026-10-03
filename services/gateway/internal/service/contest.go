package service

import (
	"context"
	"fmt"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	contestv1 "github.com/viggggil/go_oj_agent/api/contest/v1"
	gatewayv1 "github.com/viggggil/go_oj_agent/api/gateway/v1"
	gatewaymw "github.com/viggggil/go_oj_agent/services/gateway/internal/middleware"
)

func (s *GatewayService) contestClient(ctx context.Context) (contestv1.ContestServiceClient, error) {
	if _, ok := gatewaymw.RequestContextFromContext(ctx); !ok {
		return nil, gatewaymw.ErrUnauthenticated("request context is missing")
	}
	if s == nil || s.contest == nil {
		return nil, fmt.Errorf("gateway contest service is not configured")
	}
	return s.contest, nil
}

func (s *GatewayService) ListContests(ctx context.Context, req *gatewayv1.ListContestsRequest) (*gatewayv1.ListContestsResponse, error) {
	client, err := s.contestClient(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil {
		return nil, fmt.Errorf("invalid list contests request")
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	res, err := client.ListContests(ctx, &contestv1.ListContestsRequest{Page: &commonv1.PageRequest{Page: req.GetPage(), PageSize: req.GetPageSize()}, Status: req.GetStatus()})
	if err != nil {
		return nil, err
	}
	return &gatewayv1.ListContestsResponse{Items: res.GetItems(), Page: res.GetPage()}, nil
}

func (s *GatewayService) GetContest(ctx context.Context, req *gatewayv1.GetContestRequest) (*gatewayv1.GetContestResponse, error) {
	client, err := s.contestClient(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil {
		return nil, fmt.Errorf("invalid get contest request")
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	res, err := client.GetContest(ctx, &contestv1.GetContestRequest{ContestId: req.GetContestId()})
	if err != nil {
		return nil, err
	}
	return &gatewayv1.GetContestResponse{Contest: res.GetContest()}, nil
}

func (s *GatewayService) CreateContest(ctx context.Context, req *gatewayv1.CreateContestRequest) (*gatewayv1.CreateContestResponse, error) {
	client, err := s.contestClient(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil {
		return nil, fmt.Errorf("invalid create contest request")
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	res, err := client.CreateContest(ctx, &contestv1.CreateContestRequest{Contest: req.GetContest()})
	if err != nil {
		return nil, err
	}
	return &gatewayv1.CreateContestResponse{Contest: res.GetContest()}, nil
}

func (s *GatewayService) UpdateContest(ctx context.Context, req *gatewayv1.UpdateContestRequest) (*gatewayv1.UpdateContestResponse, error) {
	client, err := s.contestClient(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil {
		return nil, fmt.Errorf("invalid update contest request")
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	res, err := client.UpdateContest(ctx, &contestv1.UpdateContestRequest{ContestId: req.GetContestId(), Contest: req.GetContest()})
	if err != nil {
		return nil, err
	}
	return &gatewayv1.UpdateContestResponse{Contest: res.GetContest()}, nil
}

func (s *GatewayService) ArchiveContest(ctx context.Context, req *gatewayv1.ArchiveContestRequest) (*gatewayv1.ArchiveContestResponse, error) {
	client, err := s.contestClient(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil {
		return nil, fmt.Errorf("invalid archive contest request")
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	res, err := client.ArchiveContest(ctx, &contestv1.ArchiveContestRequest{ContestId: req.GetContestId()})
	if err != nil {
		return nil, err
	}
	return &gatewayv1.ArchiveContestResponse{Contest: res.GetContest()}, nil
}

func (s *GatewayService) GetContestLeaderboard(ctx context.Context, req *gatewayv1.GetContestLeaderboardRequest) (*gatewayv1.GetContestLeaderboardResponse, error) {
	client, err := s.contestClient(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil {
		return nil, fmt.Errorf("invalid contest leaderboard request")
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	res, err := client.GetLeaderboard(ctx, &contestv1.GetLeaderboardRequest{ContestId: req.GetContestId(), Page: &commonv1.PageRequest{Page: req.GetPage(), PageSize: req.GetPageSize()}})
	if err != nil {
		return nil, err
	}
	return &gatewayv1.GetContestLeaderboardResponse{Items: res.GetItems(), Page: res.GetPage()}, nil
}

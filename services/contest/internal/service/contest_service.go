package service

import (
	"context"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	contestv1 "github.com/viggggil/go_oj_agent/api/contest/v1"
	"github.com/viggggil/go_oj_agent/services/contest/internal/biz"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ContestService struct {
	contestv1.UnimplementedContestServiceServer
	uc *biz.ContestUsecase
}

func NewContestService(uc *biz.ContestUsecase) *ContestService {
	return &ContestService{uc: uc}
}

func (s *ContestService) GetContest(ctx context.Context, req *contestv1.GetContestRequest) (*contestv1.GetContestReply, error) {
	if req == nil || s == nil || s.uc == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid get contest request")
	}
	if err := req.Validate(); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	contest, err := s.uc.Get(ctx, req.GetContestId())
	if err != nil {
		return nil, err
	}
	return &contestv1.GetContestReply{Contest: contest}, nil
}

func (s *ContestService) ListContests(ctx context.Context, req *contestv1.ListContestsRequest) (*contestv1.ListContestsReply, error) {
	if req == nil || s == nil || s.uc == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid list contests request")
	}
	if err := req.Validate(); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	page := req.GetPage()
	if err := biz.ValidatePage(page.GetPage(), page.GetPageSize()); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	items, total, err := s.uc.List(ctx, page.GetPage(), page.GetPageSize(), req.GetStatus())
	if err != nil {
		return nil, err
	}
	return &contestv1.ListContestsReply{Items: items, Page: &commonv1.PageResponse{Page: page.GetPage(), PageSize: page.GetPageSize(), Total: total}}, nil
}

func (s *ContestService) GetLeaderboard(ctx context.Context, req *contestv1.GetLeaderboardRequest) (*contestv1.GetLeaderboardReply, error) {
	if req == nil || s == nil || s.uc == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid get leaderboard request")
	}
	if err := req.Validate(); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	page := req.GetPage()
	if err := biz.ValidatePage(page.GetPage(), page.GetPageSize()); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	items, total, err := s.uc.Leaderboard(ctx, req.GetContestId(), page.GetPage(), page.GetPageSize())
	if err != nil {
		return nil, err
	}
	return &contestv1.GetLeaderboardReply{Items: items, Page: &commonv1.PageResponse{Page: page.GetPage(), PageSize: page.GetPageSize(), Total: total}}, nil
}

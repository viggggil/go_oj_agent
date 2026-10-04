package service

import (
	"context"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	contestv1 "github.com/viggggil/go_oj_agent/api/contest/v1"
	"github.com/viggggil/go_oj_agent/pkg/internalauth"
	"github.com/viggggil/go_oj_agent/services/contest/internal/biz"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type ContestService struct {
	contestv1.UnimplementedContestServiceServer
	uc *biz.ContestUsecase
}

func NewContestService(uc *biz.ContestUsecase) *ContestService { return &ContestService{uc: uc} }

func (s *ContestService) CreateContest(ctx context.Context, req *contestv1.CreateContestRequest) (*contestv1.CreateContestReply, error) {
	if req == nil || s == nil || s.uc == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid create contest request")
	}
	if err := req.Validate(); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	actor, err := requestContext(ctx)
	if err != nil {
		return nil, err
	}
	contest, err := s.uc.Create(ctx, actor, req.GetContest())
	if err != nil {
		return nil, err
	}
	return &contestv1.CreateContestReply{Contest: toProtoContest(contest)}, nil
}

func (s *ContestService) GetContest(ctx context.Context, req *contestv1.GetContestRequest) (*contestv1.GetContestReply, error) {
	if req == nil || s == nil || s.uc == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid get contest request")
	}
	if err := req.Validate(); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	actor, err := requestContext(ctx)
	if err != nil {
		return nil, err
	}
	contest, err := s.uc.GetDetails(ctx, actor, req.GetContestId())
	if err != nil {
		return nil, err
	}
	return &contestv1.GetContestReply{Contest: toProtoContest(contest)}, nil
}

func (s *ContestService) ListContests(ctx context.Context, req *contestv1.ListContestsRequest) (*contestv1.ListContestsReply, error) {
	if req == nil || s == nil || s.uc == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid list contests request")
	}
	if err := req.Validate(); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	page := req.GetPage()
	if page.GetPage() <= 0 || page.GetPageSize() <= 0 || page.GetPageSize() > 100 {
		return nil, status.Error(codes.InvalidArgument, "invalid page")
	}
	actor, err := requestContext(ctx)
	if err != nil {
		return nil, err
	}
	items, total, err := s.uc.List(ctx, actor, page.GetPage(), page.GetPageSize(), req.GetStatus())
	if err != nil {
		return nil, err
	}
	result := make([]*contestv1.ContestSummary, 0, len(items))
	for _, item := range items {
		result = append(result, toProtoSummary(item))
	}
	return &contestv1.ListContestsReply{Items: result, Page: &commonv1.PageResponse{Page: page.GetPage(), PageSize: page.GetPageSize(), Total: total}}, nil
}

func (s *ContestService) UpdateContest(ctx context.Context, req *contestv1.UpdateContestRequest) (*contestv1.UpdateContestReply, error) {
	if req == nil || s == nil || s.uc == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid update contest request")
	}
	if err := req.Validate(); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	actor, err := requestContext(ctx)
	if err != nil {
		return nil, err
	}
	contest, err := s.uc.Update(ctx, actor, req.GetContestId(), req.GetContest())
	if err != nil {
		return nil, err
	}
	return &contestv1.UpdateContestReply{Contest: toProtoContest(contest)}, nil
}

func (s *ContestService) ArchiveContest(ctx context.Context, req *contestv1.ArchiveContestRequest) (*contestv1.ArchiveContestReply, error) {
	if req == nil || s == nil || s.uc == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid archive contest request")
	}
	if err := req.Validate(); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	actor, err := requestContext(ctx)
	if err != nil {
		return nil, err
	}
	contest, err := s.uc.Archive(ctx, actor, req.GetContestId())
	if err != nil {
		return nil, err
	}
	return &contestv1.ArchiveContestReply{Contest: toProtoContest(contest)}, nil
}

func (s *ContestService) GetLeaderboard(ctx context.Context, req *contestv1.GetLeaderboardRequest) (*contestv1.GetLeaderboardReply, error) {
	if req == nil || s == nil || s.uc == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid leaderboard request")
	}
	if err := req.Validate(); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	actor, err := requestContext(ctx)
	if err != nil {
		return nil, err
	}
	page := req.GetPage()
	items, total, err := s.uc.GetLeaderboard(ctx, actor, req.GetContestId(), page.GetPage(), page.GetPageSize())
	if err != nil {
		return nil, err
	}
	return &contestv1.GetLeaderboardReply{Items: items, Page: &commonv1.PageResponse{Page: page.GetPage(), PageSize: page.GetPageSize(), Total: total}}, nil
}

func (s *ContestService) JoinContest(ctx context.Context, req *contestv1.JoinContestRequest) (*contestv1.JoinContestReply, error) {
	if req == nil || s == nil || s.uc == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid join contest request")
	}
	if err := req.Validate(); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	actor, err := requestContext(ctx)
	if err != nil {
		return nil, err
	}
	joinedAt, err := s.uc.Join(ctx, actor, req.GetContestId())
	if err != nil {
		return nil, err
	}
	return &contestv1.JoinContestReply{ContestId: req.GetContestId(), UserId: actor.GetUserId(), JoinedAt: timestamppb.New(joinedAt)}, nil
}

func (s *ContestService) CreateContestSubmission(ctx context.Context, req *contestv1.CreateContestSubmissionRequest) (*contestv1.CreateContestSubmissionReply, error) {
	if req == nil || s == nil || s.uc == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid contest submission request")
	}
	if err := req.Validate(); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	actor, err := requestContext(ctx)
	if err != nil {
		return nil, err
	}
	result, err := s.uc.CreateSubmission(ctx, actor, req)
	if err != nil {
		return nil, err
	}
	return &contestv1.CreateContestSubmissionReply{SubmissionId: result.GetSubmissionId(), Status: result.GetStatus()}, nil
}

func requestContext(ctx context.Context) (*commonv1.RequestContext, error) {
	principal, ok := internalauth.PrincipalFromContext(ctx)
	if !ok || principal.ActorID <= 0 {
		return nil, status.Error(codes.Unauthenticated, "authenticated actor is required")
	}
	return &commonv1.RequestContext{UserId: principal.ActorID, Roles: append([]string(nil), principal.ActorRoles...), RequestId: principal.RequestID, TraceId: principal.TraceID}, nil
}

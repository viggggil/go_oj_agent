package service

import (
	"context"
	"testing"
	"time"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	contestv1 "github.com/viggggil/go_oj_agent/api/contest/v1"
	gatewayv1 "github.com/viggggil/go_oj_agent/api/gateway/v1"
	gatewaymw "github.com/viggggil/go_oj_agent/services/gateway/internal/middleware"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type fakeGatewayContestClient struct {
	contestv1.ContestServiceClient
	listRequest        *contestv1.ListContestsRequest
	updateRequest      *contestv1.UpdateContestRequest
	leaderboardRequest *contestv1.GetLeaderboardRequest
}

func (f *fakeGatewayContestClient) ListContests(_ context.Context, req *contestv1.ListContestsRequest, _ ...grpc.CallOption) (*contestv1.ListContestsReply, error) {
	f.listRequest = req
	return &contestv1.ListContestsReply{Items: []*contestv1.ContestSummary{{Id: 20}}, Page: &commonv1.PageResponse{Page: 2, PageSize: 10, Total: 1}}, nil
}

func (f *fakeGatewayContestClient) UpdateContest(_ context.Context, req *contestv1.UpdateContestRequest, _ ...grpc.CallOption) (*contestv1.UpdateContestReply, error) {
	f.updateRequest = req
	return &contestv1.UpdateContestReply{Contest: &contestv1.Contest{Id: req.GetContestId(), Title: req.GetContest().GetTitle()}}, nil
}

func (f *fakeGatewayContestClient) GetLeaderboard(_ context.Context, req *contestv1.GetLeaderboardRequest, _ ...grpc.CallOption) (*contestv1.GetLeaderboardReply, error) {
	f.leaderboardRequest = req
	return &contestv1.GetLeaderboardReply{Items: []*contestv1.LeaderboardEntry{{UserId: 42, SolvedCount: 1, PenaltySeconds: 120}}, Page: &commonv1.PageResponse{Page: 1, PageSize: 20, Total: 1}}, nil
}

func gatewayContestContext() context.Context {
	return gatewaymw.WithRequestContext(context.Background(), &commonv1.RequestContext{UserId: 42, Roles: []string{"admin"}})
}

func TestGatewayContestListForwardsPageAndStatus(t *testing.T) {
	client := &fakeGatewayContestClient{}
	service := &GatewayService{contest: client}
	response, err := service.ListContests(gatewayContestContext(), &gatewayv1.ListContestsRequest{Page: 2, PageSize: 10, Status: contestv1.ContestStatus_CONTEST_STATUS_RUNNING})
	if err != nil {
		t.Fatal(err)
	}
	if client.listRequest.GetPage().GetPage() != 2 || client.listRequest.GetPage().GetPageSize() != 10 || client.listRequest.GetStatus() != contestv1.ContestStatus_CONTEST_STATUS_RUNNING || response.GetPage().GetTotal() != 1 {
		t.Fatalf("request=%+v response=%+v", client.listRequest, response)
	}
}

func TestGatewayContestUpdateUsesPathIDAndMapsLeaderboard(t *testing.T) {
	client := &fakeGatewayContestClient{}
	service := &GatewayService{contest: client}
	start := time.Date(2026, 10, 10, 10, 0, 0, 0, time.UTC)
	update, err := service.UpdateContest(gatewayContestContext(), &gatewayv1.UpdateContestRequest{ContestId: 20, Contest: &contestv1.ContestUpdate{
		Title: "Updated", StartAt: timestamppb.New(start), EndAt: timestamppb.New(start.Add(time.Hour)),
		Problems: []*contestv1.ContestProblem{{ProblemId: 7, SortOrder: 1}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if client.updateRequest.GetContestId() != 20 || update.GetContest().GetTitle() != "Updated" {
		t.Fatalf("request=%+v response=%+v", client.updateRequest, update)
	}
	leaderboard, err := service.GetContestLeaderboard(gatewayContestContext(), &gatewayv1.GetContestLeaderboardRequest{ContestId: 20, Page: 1, PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	if client.leaderboardRequest.GetContestId() != 20 || leaderboard.GetItems()[0].GetPenaltySeconds() != 120 {
		t.Fatalf("request=%+v response=%+v", client.leaderboardRequest, leaderboard)
	}
}

func TestGatewayContestRequiresRequestContext(t *testing.T) {
	_, err := (&GatewayService{contest: &fakeGatewayContestClient{}}).GetContest(context.Background(), &gatewayv1.GetContestRequest{ContestId: 20})
	if err == nil {
		t.Fatal("expected unauthenticated error")
	}
}

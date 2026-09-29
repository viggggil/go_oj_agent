package service

import (
	"context"
	"testing"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	contestv1 "github.com/viggggil/go_oj_agent/api/contest/v1"
	"github.com/viggggil/go_oj_agent/services/contest/internal/biz"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestContestServiceRejectsInvalidRequests(t *testing.T) {
	svc := NewContestService(biz.NewContestUsecase())
	if _, err := svc.GetContest(context.Background(), &contestv1.GetContestRequest{}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("GetContest code = %v, want InvalidArgument", status.Code(err))
	}
	if _, err := svc.ListContests(context.Background(), &contestv1.ListContestsRequest{Page: &commonv1.PageRequest{Page: 1, PageSize: 101}}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("ListContests code = %v, want InvalidArgument", status.Code(err))
	}
}

func TestContestServiceDoesNotInventData(t *testing.T) {
	svc := NewContestService(biz.NewContestUsecase())
	_, err := svc.GetContest(context.Background(), &contestv1.GetContestRequest{ContestId: 1})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("GetContest code = %v, want Unimplemented", status.Code(err))
	}
	_, err = svc.GetLeaderboard(context.Background(), &contestv1.GetLeaderboardRequest{ContestId: 1, Page: &commonv1.PageRequest{Page: 1, PageSize: 20}})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("GetLeaderboard code = %v, want Unimplemented", status.Code(err))
	}
}

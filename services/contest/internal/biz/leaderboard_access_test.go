package biz

import (
	"context"
	"testing"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	contestv1 "github.com/viggggil/go_oj_agent/api/contest/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type cachedAccessRepo struct {
	fakeContestRepo
	calls    int
	expected Contest
}

func (r *cachedAccessRepo) CachedLeaderboard(_ context.Context, c Contest, _, _ int32) ([]*contestv1.LeaderboardEntry, int64, error) {
	r.calls++
	r.expected = c
	return []*contestv1.LeaderboardEntry{{UserId: 42}}, 1, nil
}
func TestLeaderboardChecksAccessBeforeCache(t *testing.T) {
	repo := &cachedAccessRepo{fakeContestRepo: fakeContestRepo{contest: Contest{ID: 20, Status: contestv1.ContestStatus_CONTEST_STATUS_ARCHIVED, Problems: []ContestProblem{{ProblemID: 7}}}}}
	usecase := NewContestUsecaseWithRepository(repo)
	for _, tc := range []struct {
		actor      *commonv1.RequestContext
		page, size int32
		code       codes.Code
	}{
		{nil, 1, 100, codes.Unauthenticated},
		{&commonv1.RequestContext{UserId: 42}, 1, 100, codes.NotFound},
		{admin(), 1, 101, codes.InvalidArgument},
	} {
		if _, _, err := usecase.GetLeaderboard(t.Context(), tc.actor, 20, tc.page, tc.size); status.Code(err) != tc.code {
			t.Fatalf("code=%v err=%v", tc.code, err)
		}
	}
	if repo.calls != 0 {
		t.Fatal("unauthorized request accessed cache")
	}
	if items, total, err := usecase.GetLeaderboard(t.Context(), admin(), 20, 1, 100); err != nil || total != 1 || len(items) != 1 || repo.calls != 1 || repo.expected.Problems[0].ProblemID != 7 {
		t.Fatalf("authorized cache read=%v %d %v", items, total, err)
	}
}

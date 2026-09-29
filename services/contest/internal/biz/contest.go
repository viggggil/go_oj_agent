package biz

import (
	"context"
	"fmt"

	contestv1 "github.com/viggggil/go_oj_agent/api/contest/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ContestUsecase struct{}

func NewContestUsecase() *ContestUsecase {
	return &ContestUsecase{}
}

func (u *ContestUsecase) Get(context.Context, int64) (*contestv1.Contest, error) {
	return nil, status.Error(codes.Unimplemented, "contest data is not implemented")
}

func (u *ContestUsecase) List(context.Context, int32, int32, contestv1.ContestStatus) ([]*contestv1.ContestSummary, int64, error) {
	return nil, 0, status.Error(codes.Unimplemented, "contest data is not implemented")
}

func (u *ContestUsecase) Update(context.Context, int64, *contestv1.ContestUpdate) (*contestv1.Contest, error) {
	return nil, status.Error(codes.Unimplemented, "contest data is not implemented")
}

func (u *ContestUsecase) Leaderboard(context.Context, int64, int32, int32) ([]*contestv1.LeaderboardEntry, int64, error) {
	return nil, 0, status.Error(codes.Unimplemented, "contest leaderboard is not implemented")
}

func ValidatePage(page, pageSize int32) error {
	if page <= 0 || pageSize <= 0 || pageSize > 100 {
		return fmt.Errorf("page must be positive and page_size must be between 1 and 100")
	}
	return nil
}

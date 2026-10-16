package biz

import (
	"context"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// 授权读取 Contest 自有数据库，不依赖列表分页、Redis 或调用方声称的比赛范围。
type KnowledgeAccessRepository interface {
	KnowledgeAllowedProblems(context.Context, []int64) ([]int64, error)
}

func (u *ContestUsecase) CheckKnowledgeAccess(ctx context.Context, actor *commonv1.RequestContext, ids []int64) ([]int64, error) {
	if actor == nil || actor.GetUserId() <= 0 {
		return nil, status.Error(codes.Unauthenticated, "authenticated actor is required")
	}
	if len(ids) == 0 || len(ids) > 100 {
		return nil, status.Error(codes.InvalidArgument, "invalid problem ids")
	}
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			return nil, status.Error(codes.InvalidArgument, "invalid problem ids")
		}
		seen[id] = true
	}
	if u == nil {
		return nil, status.Error(codes.Unavailable, "knowledge authorization unavailable")
	}
	repo, ok := u.repo.(KnowledgeAccessRepository)
	if !ok {
		return nil, status.Error(codes.Unavailable, "knowledge authorization unavailable")
	}
	return repo.KnowledgeAllowedProblems(ctx, ids)
}

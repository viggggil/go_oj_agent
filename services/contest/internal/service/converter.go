package service

import (
	contestv1 "github.com/viggggil/go_oj_agent/api/contest/v1"
	"github.com/viggggil/go_oj_agent/services/contest/internal/biz"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func toProtoContest(value biz.Contest) *contestv1.Contest {
	result := &contestv1.Contest{Id: value.ID, Title: value.Title, Status: value.Status, CreatedBy: value.CreatedBy, Joined: value.Joined, Problems: make([]*contestv1.ContestProblem, 0, len(value.Problems))}
	if !value.StartAt.IsZero() {
		result.StartAt = timestamppb.New(value.StartAt)
	}
	if !value.EndAt.IsZero() {
		result.EndAt = timestamppb.New(value.EndAt)
	}
	if !value.CreatedAt.IsZero() {
		result.CreatedAt = timestamppb.New(value.CreatedAt)
	}
	if !value.UpdatedAt.IsZero() {
		result.UpdatedAt = timestamppb.New(value.UpdatedAt)
	}
	for _, problem := range value.Problems {
		result.Problems = append(result.Problems, &contestv1.ContestProblem{ProblemId: problem.ProblemID, SortOrder: problem.SortOrder, Score: problem.Score, Title: problem.Title})
	}
	return result
}

func toProtoSummary(value biz.Contest) *contestv1.ContestSummary {
	result := &contestv1.ContestSummary{Id: value.ID, Title: value.Title, Status: value.Status}
	if !value.StartAt.IsZero() {
		result.StartAt = timestamppb.New(value.StartAt)
	}
	if !value.EndAt.IsZero() {
		result.EndAt = timestamppb.New(value.EndAt)
	}
	return result
}

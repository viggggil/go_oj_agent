package service

import (
	"context"
	"testing"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"github.com/viggggil/go_oj_agent/services/problem/internal/biz"
)

func TestListProblemsHandler(t *testing.T) {
	repo := &listServiceRepository{}
	response, err := NewProblemService(biz.NewProblemUsecase(repo)).ListProblems(userContext(), &problemv1.ListProblemsRequest{
		Page: &commonv1.PageRequest{Page: 1, PageSize: 20},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.GetItems()) != 1 || response.GetItems()[0].GetTitle() != "A+B" || response.GetPage().GetTotal() != 1 {
		t.Fatalf("ListProblems() = %+v", response)
	}
}

type listServiceRepository struct{}

func (*listServiceRepository) Create(context.Context, biz.Problem, []string) (biz.Problem, error) {
	return biz.Problem{}, nil
}
func (*listServiceRepository) FindByID(context.Context, int64) (biz.Problem, error) {
	return biz.Problem{}, nil
}
func (*listServiceRepository) List(context.Context, int32, int32, bool) ([]biz.Problem, int64, error) {
	return []biz.Problem{{ID: 1, Title: "A+B", Slug: "a-plus-b", Difficulty: problemv1.ProblemDifficulty_PROBLEM_DIFFICULTY_EASY, Status: problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL}}, 1, nil
}
func (*listServiceRepository) Update(context.Context, biz.Problem, []string) (biz.Problem, error) {
	return biz.Problem{}, nil
}
func (*listServiceRepository) Archive(context.Context, int64) (biz.Problem, error) {
	return biz.Problem{}, nil
}

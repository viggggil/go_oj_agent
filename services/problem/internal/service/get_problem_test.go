package service

import (
	"context"
	"testing"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"github.com/viggggil/go_oj_agent/services/problem/internal/biz"
)

func TestGetProblemHandler(t *testing.T) {
	repo := &serviceFakeRepository{found: biz.Problem{ID: 8, Title: "A+B", Status: problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL, Tags: []biz.Tag{{ID: 1, Name: "math"}}}}
	server := NewProblemService(biz.NewProblemUsecase(repo))
	response, err := server.GetProblem(context.Background(), &problemv1.GetProblemRequest{
		Context: &commonv1.RequestContext{UserId: 1, Roles: []string{"user"}}, ProblemId: 8,
	})
	if err != nil {
		t.Fatalf("GetProblem() error = %v", err)
	}
	if response.GetProblem().GetId() != 8 || len(response.GetProblem().GetTags()) != 1 {
		t.Fatalf("GetProblem() = %+v", response.GetProblem())
	}
}

func TestGetProblemHandlerValidatesRequest(t *testing.T) {
	server := NewProblemService(biz.NewProblemUsecase(&serviceFakeRepository{}))
	_, err := server.GetProblem(context.Background(), &problemv1.GetProblemRequest{})
	if !problemv1.IsProblemErrorReasonInvalidArgument(err) {
		t.Fatalf("expected invalid argument, got %v", err)
	}
}

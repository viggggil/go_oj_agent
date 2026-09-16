package service

import (
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"github.com/viggggil/go_oj_agent/services/problem/internal/biz"
	"testing"
)

func TestArchiveProblemHandler(t *testing.T) {
	repo := &serviceFakeRepository{found: biz.Problem{ID: 3, Status: problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL}}
	response, err := NewProblemService(biz.NewProblemUsecase(repo)).ArchiveProblem(adminContext(), &problemv1.ArchiveProblemRequest{ProblemId: 3})
	if err != nil || response.GetProblem().GetStatus() != problemv1.ProblemStatus_PROBLEM_STATUS_ARCHIVED {
		t.Fatalf("response=%+v err=%v", response, err)
	}
}

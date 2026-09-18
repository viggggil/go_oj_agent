package service

import (
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"github.com/viggggil/go_oj_agent/services/problem/internal/biz"
	"testing"
)

func TestListProblemTestcasesHandler(t *testing.T) {
	repo := &serviceTestcaseRepo{serviceFakeRepository: serviceFakeRepository{found: biz.Problem{ID: 2}}, items: []biz.Testcase{{ID: 7}}}
	response, err := NewProblemService(biz.NewProblemUsecaseWithStore(repo, repo, &serviceObjectStore{}, repo)).ListProblemTestcases(adminContext(), &problemv1.ListProblemTestcasesRequest{ProblemId: 2})
	if err != nil || len(response.GetItems()) != 1 {
		t.Fatalf("response=%+v err=%v", response, err)
	}
}

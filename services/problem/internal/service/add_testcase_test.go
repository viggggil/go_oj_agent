package service

import (
	"context"
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"github.com/viggggil/go_oj_agent/services/problem/internal/biz"
	"testing"
)

func TestAddTestcaseHandler(t *testing.T) {
	repo := &serviceTestcaseRepo{serviceFakeRepository: serviceFakeRepository{found: biz.Problem{ID: 2}}}
	objects := &serviceObjectStore{}
	server := NewProblemService(biz.NewProblemUsecaseWithStore(repo, repo, objects, repo))
	response, err := server.AddTestcase(adminContext(), &problemv1.AddTestcaseRequest{ProblemId: 2, CaseNo: 1, InputFilename: "1.in", InputContent: []byte("in"), OutputFilename: "1.out", OutputContent: []byte("out")})
	if err != nil || response.GetTestcase().GetId() != 7 {
		t.Fatalf("response=%+v err=%v", response, err)
	}
}

func TestAddTestcaseHandlerRejectsFilenameNotMatchingCaseNumber(t *testing.T) {
	repo := &serviceTestcaseRepo{serviceFakeRepository: serviceFakeRepository{found: biz.Problem{ID: 2}}}
	server := NewProblemService(biz.NewProblemUsecaseWithStore(repo, repo, &serviceObjectStore{}, repo))
	_, err := server.AddTestcase(adminContext(), &problemv1.AddTestcaseRequest{
		ProblemId: 2, CaseNo: 2,
		InputFilename: "1.in", InputContent: []byte("in"),
		OutputFilename: "2.out", OutputContent: []byte("out"),
	})
	if !problemv1.IsProblemErrorReasonInvalidArgument(err) {
		t.Fatalf("expected invalid argument, got %v", err)
	}
}

type serviceTestcaseRepo struct{ serviceFakeRepository }

func (*serviceTestcaseRepo) AddTestcase(_ context.Context, t biz.Testcase) (biz.Testcase, error) {
	t.ID = 7
	return t, nil
}
func (*serviceTestcaseRepo) ListTestcases(context.Context, int64, bool) ([]biz.Testcase, error) {
	return []biz.Testcase{{ID: 7}}, nil
}
func (*serviceTestcaseRepo) ArchiveTestcase(_ context.Context, problemID, testcaseID int64) (biz.Testcase, error) {
	return biz.Testcase{ID: testcaseID, ProblemID: problemID, Status: problemv1.TestcaseStatus_TESTCASE_STATUS_ARCHIVED}, nil
}

type serviceObjectStore struct{}

func (*serviceObjectStore) Put(context.Context, string, []byte) error { return nil }
func (*serviceObjectStore) Delete(context.Context, string) error      { return nil }

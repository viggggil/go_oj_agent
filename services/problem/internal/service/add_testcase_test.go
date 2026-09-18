package service

import (
	"context"
	"errors"
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"github.com/viggggil/go_oj_agent/services/problem/internal/biz"
	"testing"
)

func TestAddTestcaseHandler(t *testing.T) {
	repo := &serviceTestcaseRepo{serviceFakeRepository: serviceFakeRepository{found: biz.Problem{ID: 2, Status: problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL}}}
	objects := &serviceObjectStore{}
	server := NewProblemService(biz.NewProblemUsecaseWithStore(repo, repo, objects, repo))
	response, err := server.AddTestcase(adminContext(), &problemv1.AddTestcaseRequest{ProblemId: 2, CaseNo: 1, InputFilename: "1.in", InputContent: []byte("in"), OutputFilename: "1.out", OutputContent: []byte("out")})
	if err != nil || response.GetTestcase().GetId() != 7 {
		t.Fatalf("response=%+v err=%v", response, err)
	}
}

func TestAddTestcaseHandlerRejectsFilenameNotMatchingCaseNumber(t *testing.T) {
	repo := &serviceTestcaseRepo{serviceFakeRepository: serviceFakeRepository{found: biz.Problem{ID: 2, Status: problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL}}}
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

type serviceTestcaseRepo struct {
	serviceFakeRepository
	items []biz.Testcase
}

func (*serviceTestcaseRepo) AddTestcase(_ context.Context, t biz.Testcase) (biz.Testcase, error) {
	t.ID = 7
	return t, nil
}
func (r *serviceTestcaseRepo) ListTestcases(context.Context, int64, bool) ([]biz.Testcase, error) {
	return append([]biz.Testcase(nil), r.items...), nil
}
func (*serviceTestcaseRepo) ArchiveTestcase(_ context.Context, problemID, testcaseID int64) (biz.Testcase, error) {
	return biz.Testcase{ID: testcaseID, ProblemID: problemID, Status: problemv1.TestcaseStatus_TESTCASE_STATUS_ARCHIVED}, nil
}

func (r *serviceTestcaseRepo) CommitAddedTestcase(_ context.Context, testcase biz.Testcase, _, _ string) (biz.Testcase, error) {
	testcase.ID = 7
	r.items = append(r.items, testcase)
	return testcase, nil
}

func (*serviceTestcaseRepo) CommitArchivedTestcase(_ context.Context, problemID, testcaseID int64, _, _ string) (biz.Testcase, error) {
	return biz.Testcase{ID: testcaseID, ProblemID: problemID, Status: problemv1.TestcaseStatus_TESTCASE_STATUS_ARCHIVED}, nil
}

type serviceObjectStore struct{ objects map[string][]byte }

func (s *serviceObjectStore) Put(_ context.Context, key string, content []byte) error {
	return s.PutImmutable(context.Background(), key, content, "application/octet-stream")
}
func (s *serviceObjectStore) PutImmutable(_ context.Context, key string, content []byte, _ string) error {
	if s.objects == nil {
		s.objects = make(map[string][]byte)
	}
	s.objects[key] = append([]byte(nil), content...)
	return nil
}
func (s *serviceObjectStore) Get(_ context.Context, key string) ([]byte, error) {
	content, ok := s.objects[key]
	if !ok {
		return nil, errors.New("object not found")
	}
	return append([]byte(nil), content...), nil
}
func (s *serviceObjectStore) Delete(_ context.Context, key string) error {
	delete(s.objects, key)
	return nil
}

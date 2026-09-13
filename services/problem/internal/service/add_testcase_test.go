package service

import (
	"context"
	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"github.com/viggggil/go_oj_agent/services/problem/internal/biz"
	"testing"
)

func TestAddTestcaseHandler(t *testing.T) {
	repo := &serviceTestcaseRepo{serviceFakeRepository: serviceFakeRepository{found: biz.Problem{ID: 2}}}
	objects := &serviceObjectStore{}
	server := NewProblemService(biz.NewProblemUsecaseWithStore(repo, repo, objects, repo))
	response, err := server.AddTestcase(context.Background(), &problemv1.AddTestcaseRequest{Context: adminContextProto(), ProblemId: 2, CaseNo: 1, InputFilename: "1.in", InputContent: []byte("in"), OutputFilename: "1.out", OutputContent: []byte("out")})
	if err != nil || response.GetTestcase().GetId() != 7 {
		t.Fatalf("response=%+v err=%v", response, err)
	}
}
func adminContextProto() *commonv1.RequestContext {
	return &commonv1.RequestContext{UserId: 1, Roles: []string{"admin"}}
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

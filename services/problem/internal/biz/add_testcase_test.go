package biz

import (
	"context"
	"errors"
	"testing"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
)

func TestAddTestcaseUploadsAndPersistsMetadata(t *testing.T) {
	problems := &fakeProblemRepository{created: Problem{ID: 2, Status: problemv1.ProblemStatus_PROBLEM_STATUS_ARCHIVED}}
	testcases := &fakeTestcaseRepository{}
	objects := &fakeObjectStore{}
	got, err := NewProblemUsecaseWithStore(problems, testcases, objects).AddTestcase(context.Background(), adminContext(), 2, 1, []byte("1 2\n"), []byte("3\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got.InputObjectKey != "problem-2/input/1.in" || got.InputSHA256 == "" || len(objects.puts) != 2 {
		t.Fatalf("got=%+v puts=%v", got, objects.puts)
	}
}

func TestAddTestcaseCompensatesObjectsWhenMetadataFails(t *testing.T) {
	testcases := &fakeTestcaseRepository{err: errors.New("db failed")}
	objects := &fakeObjectStore{}
	_, err := NewProblemUsecaseWithStore(&fakeProblemRepository{created: Problem{ID: 2}}, testcases, objects).AddTestcase(context.Background(), adminContext(), 2, 1, []byte("in"), []byte("out"))
	if err == nil || len(objects.deletes) != 2 {
		t.Fatalf("err=%v deletes=%v", err, objects.deletes)
	}
}

func TestAddTestcaseCompensatesInputWhenOutputUploadFails(t *testing.T) {
	objects := &fakeObjectStore{putErrAt: 2}
	_, err := NewProblemUsecaseWithStore(&fakeProblemRepository{created: Problem{ID: 2}}, &fakeTestcaseRepository{}, objects).
		AddTestcase(context.Background(), adminContext(), 2, 1, []byte("in"), []byte("out"))
	if !problemv1.IsProblemErrorReasonStorageUnavailable(err) || len(objects.deletes) != 1 {
		t.Fatalf("err=%v deletes=%v", err, objects.deletes)
	}
}

func adminContext() *commonv1.RequestContext {
	return &commonv1.RequestContext{UserId: 1, Roles: []string{"admin"}}
}

type fakeTestcaseRepository struct {
	created Testcase
	err     error
}

func (r *fakeTestcaseRepository) AddTestcase(_ context.Context, t Testcase) (Testcase, error) {
	if r.err != nil {
		return Testcase{}, r.err
	}
	t.ID = 10
	r.created = t
	return t, nil
}

type fakeObjectStore struct {
	puts, deletes []string
	putErrAt      int
}

func (s *fakeObjectStore) Put(_ context.Context, key string, _ []byte) error {
	s.puts = append(s.puts, key)
	if s.putErrAt == len(s.puts) {
		return errors.New("put failed")
	}
	return nil
}
func (s *fakeObjectStore) Delete(_ context.Context, key string) error {
	s.deletes = append(s.deletes, key)
	return nil
}

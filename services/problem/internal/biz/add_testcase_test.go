package biz

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
)

func TestAddTestcaseUploadsAndPersistsMetadata(t *testing.T) {
	problems := &fakeProblemRepository{created: Problem{ID: 2, Status: problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL}}
	testcases := &fakeTestcaseRepository{}
	objects := &fakeObjectStore{}
	got, err := NewProblemUsecaseWithStore(problems, testcases, objects, problems).AddTestcase(context.Background(), adminContext(), 2, 1, []byte("1 2\n"), []byte("3\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got.InputObjectKey, "problem-2/testcases/1/") || !strings.HasSuffix(got.InputObjectKey, ".in") || got.InputSHA256 == "" || len(objects.puts) != 5 {
		t.Fatalf("got=%+v puts=%v", got, objects.puts)
	}
	inputID := strings.TrimSuffix(strings.TrimPrefix(got.InputObjectKey, "problem-2/testcases/1/"), ".in")
	if got.OutputObjectKey != "problem-2/testcases/1/"+inputID+".out" {
		t.Fatalf("object keys do not share an upload id: %+v", got)
	}
	if len(testcases.committedRevision) != 26 {
		t.Fatalf("committed revision = %q", testcases.committedRevision)
	}
	manifestKey := "problem-2/judge-revisions/" + testcases.committedRevision + "/manifest.json"
	var manifest judgeManifest
	if err := json.Unmarshal(objects.objects[manifestKey], &manifest); err != nil {
		t.Fatalf("invalid manifest: %v", err)
	}
	if manifest.JudgeRevision != testcases.committedRevision || len(manifest.Testcases) != 1 || manifest.Testcases[0].CaseNo != 1 {
		t.Fatalf("manifest = %+v", manifest)
	}
}

func TestAddTestcaseCompensatesObjectsWhenMetadataFails(t *testing.T) {
	testcases := &fakeTestcaseRepository{err: errors.New("db failed")}
	objects := &fakeObjectStore{}
	problems := &fakeProblemRepository{created: Problem{ID: 2, Status: problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL}}
	_, err := NewProblemUsecaseWithStore(problems, testcases, objects, problems).AddTestcase(context.Background(), adminContext(), 2, 1, []byte("in"), []byte("out"))
	if err == nil || len(objects.deletes) != 2 {
		t.Fatalf("err=%v deletes=%v", err, objects.deletes)
	}
}

func TestAddTestcaseCompensatesInputWhenOutputUploadFails(t *testing.T) {
	objects := &fakeObjectStore{putErrAt: 2}
	problems := &fakeProblemRepository{created: Problem{ID: 2, Status: problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL}}
	_, err := NewProblemUsecaseWithStore(problems, &fakeTestcaseRepository{}, objects, problems).
		AddTestcase(context.Background(), adminContext(), 2, 1, []byte("in"), []byte("out"))
	if !problemv1.IsProblemErrorReasonStorageUnavailable(err) || len(objects.deletes) != 1 {
		t.Fatalf("err=%v deletes=%v", err, objects.deletes)
	}
}

func adminContext() *commonv1.RequestContext {
	return &commonv1.RequestContext{UserId: 1, Roles: []string{"admin"}}
}

type fakeTestcaseRepository struct {
	created           Testcase
	items             []Testcase
	committedRevision string
	err               error
}

func (r *fakeTestcaseRepository) AddTestcase(_ context.Context, t Testcase) (Testcase, error) {
	if r.err != nil {
		return Testcase{}, r.err
	}
	t.ID = 10
	r.created = t
	return t, nil
}
func (r *fakeTestcaseRepository) ListTestcases(context.Context, int64, bool) ([]Testcase, error) {
	if r.items != nil {
		return append([]Testcase(nil), r.items...), r.err
	}
	if r.created.ID == 0 {
		return nil, r.err
	}
	return []Testcase{r.created}, r.err
}
func (r *fakeTestcaseRepository) ArchiveTestcase(_ context.Context, problemID, testcaseID int64) (Testcase, error) {
	r.created.ID = testcaseID
	r.created.ProblemID = problemID
	r.created.Status = problemv1.TestcaseStatus_TESTCASE_STATUS_ARCHIVED
	return r.created, r.err
}

func (r *fakeTestcaseRepository) CommitAddedTestcase(_ context.Context, testcase Testcase, revision, _ string) (Testcase, error) {
	if r.err != nil {
		return Testcase{}, r.err
	}
	testcase.ID = 10
	r.created = testcase
	r.committedRevision = revision
	return testcase, nil
}

func (r *fakeTestcaseRepository) CommitArchivedTestcase(_ context.Context, problemID, testcaseID int64, revision, _ string) (Testcase, error) {
	if r.err != nil {
		return Testcase{}, r.err
	}
	r.created.ID = testcaseID
	r.created.ProblemID = problemID
	r.created.Status = problemv1.TestcaseStatus_TESTCASE_STATUS_ARCHIVED
	r.committedRevision = revision
	return r.created, nil
}

type fakeObjectStore struct {
	puts, deletes []string
	putErrAt      int
	objects       map[string][]byte
}

func (s *fakeObjectStore) Put(_ context.Context, key string, content []byte) error {
	return s.put(key, content)
}
func (s *fakeObjectStore) PutImmutable(_ context.Context, key string, content []byte, _ string) error {
	return s.put(key, content)
}
func (s *fakeObjectStore) put(key string, content []byte) error {
	s.puts = append(s.puts, key)
	if s.putErrAt == len(s.puts) {
		return errors.New("put failed")
	}
	if s.objects == nil {
		s.objects = make(map[string][]byte)
	}
	s.objects[key] = append([]byte(nil), content...)
	return nil
}
func (s *fakeObjectStore) Get(_ context.Context, key string) ([]byte, error) {
	content, ok := s.objects[key]
	if !ok {
		return nil, errors.New("object not found")
	}
	return append([]byte(nil), content...), nil
}
func (s *fakeObjectStore) Delete(_ context.Context, key string) error {
	s.deletes = append(s.deletes, key)
	delete(s.objects, key)
	return nil
}

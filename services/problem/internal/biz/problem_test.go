package biz

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	judgecontract "github.com/viggggil/go_oj_agent/pkg/judge"
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
	var manifest judgecontract.Manifest
	if err := json.Unmarshal(objects.objects[manifestKey], &manifest); err != nil {
		t.Fatalf("invalid manifest: %v", err)
	}
	if manifest.ManifestVersion != judgecontract.ManifestVersion || manifest.JudgeRevision != testcases.committedRevision ||
		manifest.TimeLimitMS != 1000 || manifest.MemoryLimitKB != 65536 || len(manifest.Testcases) != 1 || manifest.Testcases[0].CaseNo != 1 {
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
func TestArchiveProblem(t *testing.T) {
	repo := &fakeProblemRepository{created: Problem{ID: 3, Status: problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL}}
	got, err := NewProblemUsecase(repo).Archive(context.Background(), &commonv1.RequestContext{UserId: 1, Roles: []string{"admin"}}, 3)
	if err != nil || got.Status != problemv1.ProblemStatus_PROBLEM_STATUS_ARCHIVED || !repo.archived {
		t.Fatalf("Archive() = %+v, %v", got, err)
	}
}
func TestArchiveProblemIsIdempotent(t *testing.T) {
	repo := &fakeProblemRepository{created: Problem{ID: 3, Status: problemv1.ProblemStatus_PROBLEM_STATUS_ARCHIVED}}
	_, err := NewProblemUsecase(repo).Archive(context.Background(), &commonv1.RequestContext{UserId: 1, Roles: []string{"admin"}}, 3)
	if err != nil || repo.archived {
		t.Fatalf("err=%v archived=%v", err, repo.archived)
	}
}
func TestArchiveTestcase(t *testing.T) {
	problems := &fakeProblemRepository{created: Problem{ID: 2, Status: problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL, ActiveJudgeRevision: "01K5C6Y7N8P9Q0R1S2T3V4W5X6"}}
	content := []byte("case")
	hash := sha256.Sum256(content)
	repo := &fakeTestcaseRepository{items: []Testcase{
		{ID: 7, ProblemID: 2, CaseNo: 1, Status: problemv1.TestcaseStatus_TESTCASE_STATUS_ACTIVE},
		{ID: 8, ProblemID: 2, CaseNo: 2, InputObjectKey: "in", OutputObjectKey: "out", InputSHA256: fmt.Sprintf("%x", hash), OutputSHA256: fmt.Sprintf("%x", hash), InputSizeBytes: int64(len(content)), OutputSizeBytes: int64(len(content)), Status: problemv1.TestcaseStatus_TESTCASE_STATUS_ACTIVE},
	}}
	objects := &fakeObjectStore{objects: map[string][]byte{"in": content, "out": content}}
	got, err := NewProblemUsecaseWithStore(problems, repo, objects, problems).ArchiveTestcase(context.Background(), adminContext(), 2, 7)
	if err != nil || got.Status != problemv1.TestcaseStatus_TESTCASE_STATUS_ARCHIVED {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}
func TestCreateProblem(t *testing.T) {
	repo := &fakeProblemRepository{created: Problem{ID: 42}}
	uc := NewProblemUsecase(repo)
	input := validCreateInput()
	input.Problem.Title = "  Two Sum  "
	input.Tags = []string{" Array ", "array", "Hash"}

	_, err := uc.Create(context.Background(), input)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if repo.input.Title != "Two Sum" || repo.input.Status != problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL || repo.input.CreatedBy != 7 {
		t.Fatalf("repository problem = %+v", repo.input)
	}
	if len(repo.tags) != 2 || repo.tags[0] != "array" || repo.tags[1] != "hash" {
		t.Fatalf("repository tags = %v", repo.tags)
	}
}

func TestCreateProblemRejectsNonAdmin(t *testing.T) {
	input := validCreateInput()
	input.Context.Roles = []string{"user"}
	_, err := NewProblemUsecase(&fakeProblemRepository{}).Create(context.Background(), input)
	if !problemv1.IsProblemErrorReasonPermissionDenied(err) {
		t.Fatalf("expected permission denied, got %v", err)
	}
}

func TestCreateProblemStoresTestcases(t *testing.T) {
	input := validCreateInput()
	input.Testcases = []TestcaseContent{{CaseNo: 1, Input: []byte("in"), Output: []byte("out")}}
	problems := &fakeProblemRepository{created: Problem{ID: 5, Status: problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL}}
	testcases := &fakeTestcaseRepository{}
	objects := &fakeObjectStore{}
	_, err := NewProblemUsecaseWithStore(problems, testcases, objects, problems).Create(context.Background(), input)
	if err != nil || testcases.created.ProblemID != 5 || len(objects.puts) != 5 {
		t.Fatalf("error=%v testcase=%+v puts=%v", err, testcases.created, objects.puts)
	}
}

func TestCreateProblemPropagatesRepositoryError(t *testing.T) {
	want := errors.New("database unavailable")
	_, err := NewProblemUsecase(&fakeProblemRepository{err: want}).Create(context.Background(), validCreateInput())
	if !errors.Is(err, want) {
		t.Fatalf("Create() error = %v, want %v", err, want)
	}
}

func validCreateInput() CreateProblemInput {
	return CreateProblemInput{
		Context: &commonv1.RequestContext{UserId: 7, Roles: []string{"admin"}},
		Problem: Problem{Title: "Two Sum", Slug: "two-sum", Description: "Find two values.",
			Difficulty: problemv1.ProblemDifficulty_PROBLEM_DIFFICULTY_EASY, TimeLimitMs: 1000, MemoryLimitKb: 65536},
	}
}

type fakeProblemRepository struct {
	input    Problem
	tags     []string
	created  Problem
	err      error
	archived bool
}

func (r *fakeProblemRepository) Create(_ context.Context, problem Problem, tags []string) (Problem, error) {
	r.input = problem
	r.tags = append([]string(nil), tags...)
	if r.err != nil {
		return Problem{}, r.err
	}
	if r.created.ID == 0 {
		r.created = problem
	} else {
		if r.created.TimeLimitMs == 0 {
			r.created.TimeLimitMs = problem.TimeLimitMs
		}
		if r.created.MemoryLimitKb == 0 {
			r.created.MemoryLimitKb = problem.MemoryLimitKb
		}
	}
	return r.created, nil
}

func (r *fakeProblemRepository) FindByID(context.Context, int64) (Problem, error) {
	if r.created.TimeLimitMs == 0 {
		r.created.TimeLimitMs = 1000
	}
	if r.created.MemoryLimitKb == 0 {
		r.created.MemoryLimitKb = 65536
	}
	return r.created, r.err
}

func (r *fakeProblemRepository) List(context.Context, int32, int32, bool) ([]Problem, int64, error) {
	return nil, 0, r.err
}
func (r *fakeProblemRepository) Update(_ context.Context, problem Problem, tags []string, _ string) (Problem, error) {
	r.input, r.tags = problem, tags
	return problem, r.err
}
func (r *fakeProblemRepository) Archive(context.Context, int64) (Problem, error) {
	r.archived = true
	r.created.Status = problemv1.ProblemStatus_PROBLEM_STATUS_ARCHIVED
	return r.created, r.err
}
func (r *fakeProblemRepository) DeleteCreatedProblem(context.Context, int64) error { return r.err }
func TestGetProblemVisibility(t *testing.T) {
	user := &commonv1.RequestContext{UserId: 1, Roles: []string{"user"}}
	admin := &commonv1.RequestContext{UserId: 2, Roles: []string{"admin"}}

	for _, status := range []problemv1.ProblemStatus{problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL, problemv1.ProblemStatus_PROBLEM_STATUS_ARCHIVED} {
		repo := &fakeProblemRepository{created: Problem{ID: 10, Status: status}}
		_, userErr := NewProblemUsecase(repo).Get(context.Background(), user, 10)
		if status == problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL && userErr != nil {
			t.Fatalf("normal problem rejected: %v", userErr)
		}
		if status == problemv1.ProblemStatus_PROBLEM_STATUS_ARCHIVED && !problemv1.IsProblemErrorReasonNotFound(userErr) {
			t.Fatalf("expected archived problem hidden, got %v", userErr)
		}
		if _, err := NewProblemUsecase(repo).Get(context.Background(), admin, 10); err != nil {
			t.Fatalf("admin get status %s: %v", status, err)
		}
	}
}
func TestGetJudgeProfile(t *testing.T) {
	problem := Problem{ID: 2, Status: problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL, TimeLimitMs: 1000, MemoryLimitKb: 65536, ActiveJudgeRevision: "01K5C6Y7N8P9Q0R1S2T3V4W5X6"}
	testcases := &fakeTestcaseRepository{items: []Testcase{{ID: 1, ProblemID: 2, Status: problemv1.TestcaseStatus_TESTCASE_STATUS_ACTIVE}}}
	got, err := NewProblemUsecaseWithStore(&fakeProblemRepository{created: problem}, testcases, &fakeObjectStore{}, &fakeProblemRepository{}).GetJudgeProfile(context.Background(), 2)
	if err != nil || got.ActiveJudgeRevision != problem.ActiveJudgeRevision {
		t.Fatalf("GetJudgeProfile() = %+v, %v", got, err)
	}
}

func TestGetJudgeProfileRejectsUnavailableProblem(t *testing.T) {
	tests := []struct {
		name      string
		problem   Problem
		testcases []Testcase
	}{
		{name: "archived", problem: Problem{ID: 2, Status: problemv1.ProblemStatus_PROBLEM_STATUS_ARCHIVED, ActiveJudgeRevision: "01K5C6Y7N8P9Q0R1S2T3V4W5X6"}, testcases: []Testcase{{ID: 1}}},
		{name: "missing revision", problem: Problem{ID: 2, Status: problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL}, testcases: []Testcase{{ID: 1}}},
		{name: "empty testcases", problem: Problem{ID: 2, Status: problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL, ActiveJudgeRevision: "01K5C6Y7N8P9Q0R1S2T3V4W5X6"}, testcases: []Testcase{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewProblemUsecaseWithStore(&fakeProblemRepository{created: tt.problem}, &fakeTestcaseRepository{items: tt.testcases}, &fakeObjectStore{}, &fakeProblemRepository{}).GetJudgeProfile(context.Background(), 2)
			if !problemv1.IsProblemErrorReasonInvalidStatus(err) {
				t.Fatalf("expected invalid status, got %v", err)
			}
		})
	}
}
func TestListProblemsNormalizesPaginationAndVisibility(t *testing.T) {
	repo := &listRepository{}
	uc := NewProblemUsecase(repo)
	page, err := uc.List(context.Background(), &commonv1.RequestContext{UserId: 1, Roles: []string{"user"}}, 0, 500)
	if err != nil {
		t.Fatal(err)
	}
	if page.Page != 1 || page.PageSize != 100 || repo.includeArchived {
		t.Fatalf("page = %+v, includeArchived = %v", page, repo.includeArchived)
	}
	_, err = uc.List(context.Background(), &commonv1.RequestContext{UserId: 2, Roles: []string{"admin"}}, 2, 10)
	if err != nil || !repo.includeArchived {
		t.Fatalf("admin list error = %v, includeArchived = %v", err, repo.includeArchived)
	}
}

type listRepository struct{ includeArchived bool }

func (*listRepository) Create(context.Context, Problem, []string) (Problem, error) {
	return Problem{}, nil
}
func (*listRepository) FindByID(context.Context, int64) (Problem, error) { return Problem{}, nil }
func (r *listRepository) List(_ context.Context, _, _ int32, includeArchived bool) ([]Problem, int64, error) {
	r.includeArchived = includeArchived
	return []Problem{{ID: 1}}, 1, nil
}
func (*listRepository) Update(context.Context, Problem, []string, string) (Problem, error) {
	return Problem{}, nil
}
func (*listRepository) Archive(context.Context, int64) (Problem, error) { return Problem{}, nil }
func TestListTestcasesAllowsAdminAndJudge(t *testing.T) {
	for _, role := range []string{"admin", "judge"} {
		problems := &fakeProblemRepository{created: Problem{ID: 2}}
		repo := &fakeTestcaseRepository{created: Testcase{ID: 1}}
		items, err := NewProblemUsecaseWithStore(problems, repo, &fakeObjectStore{}, problems).ListTestcases(context.Background(), &commonv1.RequestContext{UserId: 1, Roles: []string{role}}, 2, false)
		if err != nil || len(items) != 1 {
			t.Fatalf("role=%s items=%v err=%v", role, items, err)
		}
	}
}
func TestListTestcasesRejectsUser(t *testing.T) {
	problems := &fakeProblemRepository{}
	_, err := NewProblemUsecaseWithStore(problems, &fakeTestcaseRepository{}, &fakeObjectStore{}, problems).ListTestcases(context.Background(), &commonv1.RequestContext{UserId: 1, Roles: []string{"user"}}, 2, false)
	if err == nil {
		t.Fatal("expected permission error")
	}
}

type fakeProblemCache struct {
	problem       Problem
	found         bool
	sets, deletes int
}

func (c *fakeProblemCache) Get(context.Context, int64) (Problem, bool, error) {
	return c.problem, c.found, nil
}
func (c *fakeProblemCache) Set(_ context.Context, p Problem) error {
	c.problem = p
	c.sets++
	return nil
}
func (c *fakeProblemCache) Delete(context.Context, int64) error { c.deletes++; return nil }
func TestGetProblemUsesCache(t *testing.T) {
	cache := &fakeProblemCache{problem: Problem{ID: 2, Status: problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL}, found: true}
	repo := &fakeProblemRepository{err: context.Canceled}
	got, err := NewProblemUsecaseWithDependencies(repo, nil, nil, nil, cache).Get(context.Background(), &commonv1.RequestContext{UserId: 1, Roles: []string{"user"}}, 2)
	if err != nil || got.ID != 2 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}
func TestUpdateProblemInvalidatesCache(t *testing.T) {
	cache := &fakeProblemCache{}
	repo := &fakeProblemRepository{created: Problem{ID: 2, Status: problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL}}
	_, err := NewProblemUsecaseWithDependencies(repo, nil, nil, nil, cache).Update(context.Background(), adminContext(), 2, Problem{Title: "A", Slug: "a", Description: "S", Difficulty: problemv1.ProblemDifficulty_PROBLEM_DIFFICULTY_EASY, TimeLimitMs: 1, MemoryLimitKb: 1}, nil)
	if err != nil || cache.deletes != 1 {
		t.Fatalf("err=%v deletes=%d", err, cache.deletes)
	}
}

func TestUpdateProblemLimitsPublishesNewJudgeRevision(t *testing.T) {
	inputContent := []byte("1 2\n")
	outputContent := []byte("3\n")
	inputHash := sha256.Sum256(inputContent)
	outputHash := sha256.Sum256(outputContent)
	oldRevision := "01K5C6Y7N8P9Q0R1S2T3V4W5X6"
	problem := Problem{
		ID: 2, Title: "A+B", Slug: "a-plus-b", Description: "Add.",
		Difficulty:  problemv1.ProblemDifficulty_PROBLEM_DIFFICULTY_EASY,
		TimeLimitMs: 1000, MemoryLimitKb: 65536, ActiveJudgeRevision: oldRevision,
		Status: problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL, CreatedBy: 1,
	}
	testcases := &fakeTestcaseRepository{items: []Testcase{{
		ID: 1, ProblemID: 2, CaseNo: 1, InputObjectKey: "source/1.in", OutputObjectKey: "source/1.out",
		InputSHA256: fmt.Sprintf("%x", inputHash), OutputSHA256: fmt.Sprintf("%x", outputHash),
		InputSizeBytes: int64(len(inputContent)), OutputSizeBytes: int64(len(outputContent)),
		Status: problemv1.TestcaseStatus_TESTCASE_STATUS_ACTIVE,
	}}}
	objects := &fakeObjectStore{objects: map[string][]byte{"source/1.in": inputContent, "source/1.out": outputContent}}
	repo := &fakeProblemRepository{created: problem}
	input := problem
	input.TimeLimitMs = 2000
	input.MemoryLimitKb = 131072

	updated, err := NewProblemUsecaseWithDependencies(repo, testcases, objects, repo, nil).
		Update(context.Background(), adminContext(), problem.ID, input, nil)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.ActiveJudgeRevision == "" || updated.ActiveJudgeRevision == oldRevision {
		t.Fatalf("active revision = %q", updated.ActiveJudgeRevision)
	}
	manifestKey := fmt.Sprintf("problem-%d/judge-revisions/%s/manifest.json", problem.ID, updated.ActiveJudgeRevision)
	var manifest judgecontract.Manifest
	if err := json.Unmarshal(objects.objects[manifestKey], &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if manifest.TimeLimitMS != 2000 || manifest.MemoryLimitKB != 131072 || len(manifest.Testcases) != 1 {
		t.Fatalf("manifest = %+v", manifest)
	}
}

func TestUpdateProblemMetadataKeepsJudgeRevision(t *testing.T) {
	problem := Problem{
		ID: 2, Title: "A+B", Slug: "a-plus-b", Description: "Add.",
		Difficulty:  problemv1.ProblemDifficulty_PROBLEM_DIFFICULTY_EASY,
		TimeLimitMs: 1000, MemoryLimitKb: 65536,
		ActiveJudgeRevision: "01K5C6Y7N8P9Q0R1S2T3V4W5X6",
		Status:              problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL,
	}
	repo := &fakeProblemRepository{created: problem}
	input := problem
	input.Title = "New title"
	objects := &fakeObjectStore{}
	updated, err := NewProblemUsecaseWithDependencies(repo, &fakeTestcaseRepository{}, objects, repo, nil).
		Update(context.Background(), adminContext(), problem.ID, input, nil)
	if err != nil || updated.ActiveJudgeRevision != problem.ActiveJudgeRevision || len(objects.puts) != 0 {
		t.Fatalf("updated=%+v puts=%v err=%v", updated, objects.puts, err)
	}
}

func TestCreateProblemFailureInvalidatesPopulatedCache(t *testing.T) {
	cache := &fakeProblemCache{}
	problems := &fakeProblemRepository{created: Problem{ID: 5, Status: problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL}}
	testcases := &fakeTestcaseRepository{err: errors.New("db failed")}
	input := validCreateInput()
	input.Testcases = []TestcaseContent{{CaseNo: 1, Input: []byte("in"), Output: []byte("out")}}
	_, err := NewProblemUsecaseWithDependencies(problems, testcases, &fakeObjectStore{}, problems, cache).Create(context.Background(), input)
	if err == nil || cache.sets != 1 || cache.deletes != 1 {
		t.Fatalf("err=%v sets=%d deletes=%d", err, cache.sets, cache.deletes)
	}
}
func TestRequireAdmin(t *testing.T) {
	if err := requireAdmin(&commonv1.RequestContext{UserId: 1, Roles: []string{" ADMIN "}}); err != nil {
		t.Fatalf("admin context rejected: %v", err)
	}
	if err := requireAdmin(&commonv1.RequestContext{UserId: 1, Roles: []string{"user"}}); !problemv1.IsProblemErrorReasonPermissionDenied(err) {
		t.Fatalf("expected permission denied, got %v", err)
	}
	if err := requireAdmin(nil); !problemv1.IsProblemErrorReasonInvalidArgument(err) {
		t.Fatalf("expected invalid argument, got %v", err)
	}
}

func TestNormalizeTags(t *testing.T) {
	got := normalizeTags([]string{" DP ", "dp", "", "Graph"})
	want := []string{"dp", "graph"}
	if len(got) != len(want) {
		t.Fatalf("normalizeTags() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("normalizeTags() = %v, want %v", got, want)
		}
	}
}
func TestUpdateProblem(t *testing.T) {
	repo := &fakeProblemRepository{created: Problem{ID: 4, Status: problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL, CreatedBy: 3}}
	updated, err := NewProblemUsecase(repo).Update(context.Background(), &commonv1.RequestContext{UserId: 1, Roles: []string{"admin"}}, 4,
		Problem{Title: " New ", Slug: "new", Description: " Text ", Difficulty: problemv1.ProblemDifficulty_PROBLEM_DIFFICULTY_MEDIUM, TimeLimitMs: 2000, MemoryLimitKb: 65536}, []string{" DP "})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != "New" || updated.CreatedBy != 3 || len(repo.tags) != 1 || repo.tags[0] != "dp" {
		t.Fatalf("Update() = %+v, tags %v", updated, repo.tags)
	}
}

func TestUpdateProblemRejectsArchived(t *testing.T) {
	repo := &fakeProblemRepository{created: Problem{ID: 4, Status: problemv1.ProblemStatus_PROBLEM_STATUS_ARCHIVED}}
	_, err := NewProblemUsecase(repo).Update(context.Background(), &commonv1.RequestContext{UserId: 1, Roles: []string{"admin"}}, 4, Problem{}, nil)
	if !problemv1.IsProblemErrorReasonInvalidStatus(err) {
		t.Fatalf("expected invalid status, got %v", err)
	}
}

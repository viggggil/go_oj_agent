package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"github.com/viggggil/go_oj_agent/pkg/internalauth"
	"github.com/viggggil/go_oj_agent/services/problem/internal/biz"
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
func TestArchiveProblemHandler(t *testing.T) {
	repo := &serviceFakeRepository{found: biz.Problem{ID: 3, Status: problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL}}
	response, err := NewProblemService(biz.NewProblemUsecase(repo)).ArchiveProblem(adminContext(), &problemv1.ArchiveProblemRequest{ProblemId: 3})
	if err != nil || response.GetProblem().GetStatus() != problemv1.ProblemStatus_PROBLEM_STATUS_ARCHIVED {
		t.Fatalf("response=%+v err=%v", response, err)
	}
}
func TestArchiveTestcaseHandler(t *testing.T) {
	content := []byte("case")
	hash := sha256.Sum256(content)
	repo := &serviceTestcaseRepo{
		serviceFakeRepository: serviceFakeRepository{found: biz.Problem{ID: 2, Status: problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL, ActiveJudgeRevision: "01K5C6Y7N8P9Q0R1S2T3V4W5X6"}},
		items: []biz.Testcase{
			{ID: 7, ProblemID: 2, CaseNo: 1, Status: problemv1.TestcaseStatus_TESTCASE_STATUS_ACTIVE},
			{ID: 8, ProblemID: 2, CaseNo: 2, InputObjectKey: "in", OutputObjectKey: "out", InputSHA256: fmt.Sprintf("%x", hash), OutputSHA256: fmt.Sprintf("%x", hash), InputSizeBytes: int64(len(content)), OutputSizeBytes: int64(len(content)), Status: problemv1.TestcaseStatus_TESTCASE_STATUS_ACTIVE},
		},
	}
	objects := &serviceObjectStore{objects: map[string][]byte{"in": content, "out": content}}
	response, err := NewProblemService(biz.NewProblemUsecaseWithStore(repo, repo, objects, repo)).ArchiveTestcase(adminContext(), &problemv1.ArchiveTestcaseRequest{ProblemId: 2, TestcaseId: 7})
	if err != nil || response.GetTestcase().GetStatus() != problemv1.TestcaseStatus_TESTCASE_STATUS_ARCHIVED {
		t.Fatalf("response=%+v err=%v", response, err)
	}
}
func TestCreateProblemHandler(t *testing.T) {
	repo := &serviceFakeRepository{}
	server := NewProblemService(biz.NewProblemUsecase(repo))
	response, err := server.CreateProblem(internalauth.WithPrincipal(context.Background(), internalauth.Principal{ActorID: 9, ActorRoles: []string{"admin"}}), &problemv1.CreateProblemRequest{
		Problem: &problemv1.ProblemInput{Title: "Two Sum", Slug: "two-sum", Description: "Statement",
			Difficulty: problemv1.ProblemDifficulty_PROBLEM_DIFFICULTY_EASY, TimeLimitMs: 1000, MemoryLimitKb: 65536},
	})
	if err != nil {
		t.Fatalf("CreateProblem() error = %v", err)
	}
	if response.GetProblem().GetId() != 101 || response.GetProblem().GetCreatedBy() != 9 || response.GetProblem().GetStatus() != problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL {
		t.Fatalf("CreateProblem() response = %+v", response.GetProblem())
	}
}

func TestCreateProblemHandlerValidatesRequest(t *testing.T) {
	server := NewProblemService(biz.NewProblemUsecase(&serviceFakeRepository{}))
	_, err := server.CreateProblem(context.Background(), &problemv1.CreateProblemRequest{})
	if !problemv1.IsProblemErrorReasonInvalidArgument(err) {
		t.Fatalf("expected invalid argument, got %v", err)
	}
}

func TestCreateProblemHandlerForwardsTestcases(t *testing.T) {
	repo := &serviceTestcaseRepo{serviceFakeRepository: serviceFakeRepository{}}
	objects := &serviceObjectStore{}
	server := NewProblemService(biz.NewProblemUsecaseWithStore(repo, repo, objects, repo))
	response, err := server.CreateProblem(adminContext(), &problemv1.CreateProblemRequest{
		Problem:   &problemv1.ProblemInput{Title: "A+B", Slug: "a-plus-b", Description: "Statement", Difficulty: problemv1.ProblemDifficulty_PROBLEM_DIFFICULTY_EASY, TimeLimitMs: 1000, MemoryLimitKb: 65536},
		Testcases: []*problemv1.TestcaseInput{{CaseNo: 1, InputFilename: "1.in", InputContent: []byte("in"), OutputFilename: "1.out", OutputContent: []byte("out")}},
	})
	if err != nil || response.GetProblem().GetId() != 101 {
		t.Fatalf("response=%+v err=%v", response, err)
	}
}

type serviceFakeRepository struct {
	found biz.Problem
	err   error
}

func (r *serviceFakeRepository) Create(_ context.Context, problem biz.Problem, _ []string) (biz.Problem, error) {
	problem.ID = 101
	r.found = problem
	return problem, nil
}

func (r *serviceFakeRepository) FindByID(context.Context, int64) (biz.Problem, error) {
	if r.found.TimeLimitMs == 0 {
		r.found.TimeLimitMs = 1000
	}
	if r.found.MemoryLimitKb == 0 {
		r.found.MemoryLimitKb = 65536
	}
	return r.found, r.err
}

func (r *serviceFakeRepository) List(context.Context, int32, int32, bool) ([]biz.Problem, int64, error) {
	return nil, 0, r.err
}
func (r *serviceFakeRepository) Update(_ context.Context, problem biz.Problem, _ []string, _ string) (biz.Problem, error) {
	return problem, r.err
}
func (r *serviceFakeRepository) Archive(context.Context, int64) (biz.Problem, error) {
	r.found.Status = problemv1.ProblemStatus_PROBLEM_STATUS_ARCHIVED
	return r.found, r.err
}
func (r *serviceFakeRepository) DeleteCreatedProblem(context.Context, int64) error { return r.err }
func TestGetJudgeProfileHandler(t *testing.T) {
	repo := &serviceTestcaseRepo{
		serviceFakeRepository: serviceFakeRepository{found: biz.Problem{ID: 2, Status: problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL, TimeLimitMs: 1000, MemoryLimitKb: 65536, ActiveJudgeRevision: "01K5C6Y7N8P9Q0R1S2T3V4W5X6"}},
		items:                 []biz.Testcase{{ID: 1, ProblemID: 2, Status: problemv1.TestcaseStatus_TESTCASE_STATUS_ACTIVE}},
	}
	ctx := internalauth.WithPrincipal(context.Background(), internalauth.Principal{Caller: "judge-service"})
	response, err := NewProblemService(biz.NewProblemUsecaseWithStore(repo, repo, &serviceObjectStore{}, repo)).GetJudgeProfile(ctx, &problemv1.GetJudgeProfileRequest{ProblemId: 2})
	if err != nil || response.GetProfile().GetActiveJudgeRevision() != "01K5C6Y7N8P9Q0R1S2T3V4W5X6" {
		t.Fatalf("response=%+v err=%v", response, err)
	}
}

func TestGetJudgeProfileRejectsUntrustedCaller(t *testing.T) {
	repo := &serviceTestcaseRepo{}
	server := NewProblemService(biz.NewProblemUsecaseWithStore(repo, repo, &serviceObjectStore{}, repo))
	_, err := server.GetJudgeProfile(context.Background(), &problemv1.GetJudgeProfileRequest{ProblemId: 2})
	if err == nil {
		t.Fatal("expected missing principal to be rejected")
	}
	ctx := internalauth.WithPrincipal(context.Background(), internalauth.Principal{Caller: "gateway-service", ActorID: 1, ActorRoles: []string{"admin"}})
	_, err = server.GetJudgeProfile(ctx, &problemv1.GetJudgeProfileRequest{ProblemId: 2})
	if !problemv1.IsProblemErrorReasonPermissionDenied(err) {
		t.Fatalf("expected permission denied, got %v", err)
	}
}
func TestGetProblemHandler(t *testing.T) {
	repo := &serviceFakeRepository{found: biz.Problem{ID: 8, Title: "A+B", Status: problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL, Tags: []biz.Tag{{ID: 1, Name: "math"}}}}
	server := NewProblemService(biz.NewProblemUsecase(repo))
	response, err := server.GetProblem(userContext(), &problemv1.GetProblemRequest{
		ProblemId: 8,
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
func (*listServiceRepository) Update(context.Context, biz.Problem, []string, string) (biz.Problem, error) {
	return biz.Problem{}, nil
}
func (*listServiceRepository) Archive(context.Context, int64) (biz.Problem, error) {
	return biz.Problem{}, nil
}
func TestListProblemTestcasesHandler(t *testing.T) {
	repo := &serviceTestcaseRepo{serviceFakeRepository: serviceFakeRepository{found: biz.Problem{ID: 2}}, items: []biz.Testcase{{ID: 7}}}
	response, err := NewProblemService(biz.NewProblemUsecaseWithStore(repo, repo, &serviceObjectStore{}, repo)).ListProblemTestcases(adminContext(), &problemv1.ListProblemTestcasesRequest{ProblemId: 2})
	if err != nil || len(response.GetItems()) != 1 {
		t.Fatalf("response=%+v err=%v", response, err)
	}
}
func adminContext() context.Context {
	return internalauth.WithPrincipal(context.Background(), internalauth.Principal{Caller: "gateway-service", ActorID: 1, ActorRoles: []string{"admin"}})
}
func userContext() context.Context {
	return internalauth.WithPrincipal(context.Background(), internalauth.Principal{Caller: "gateway-service", ActorID: 1, ActorRoles: []string{"user"}})
}
func TestUpdateProblemHandler(t *testing.T) {
	repo := &serviceFakeRepository{found: biz.Problem{ID: 3, Status: problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL, CreatedBy: 1}}
	response, err := NewProblemService(biz.NewProblemUsecase(repo)).UpdateProblem(adminContext(), &problemv1.UpdateProblemRequest{ProblemId: 3, Problem: &problemv1.ProblemInput{Title: "New", Slug: "new", Description: "Statement", Difficulty: problemv1.ProblemDifficulty_PROBLEM_DIFFICULTY_MEDIUM, TimeLimitMs: 1000, MemoryLimitKb: 65536}})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetProblem().GetTitle() != "New" {
		t.Fatalf("response = %+v", response)
	}
}

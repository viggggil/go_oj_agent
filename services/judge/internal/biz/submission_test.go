package biz

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	submissionv1 "github.com/viggggil/go_oj_agent/api/submission/v1"
)

const testRevision = "01K5C6Y7N8P9Q0R1S2T3V4W5X6"

func TestListFilterNormalized(t *testing.T) {
	got := (ListFilter{Page: -1, PageSize: 1000, Language: " Go "}).Normalized()
	if got.Page != 1 || got.PageSize != MaxPageSize || got.Language != "go" {
		t.Fatalf("Normalized() = %+v", got)
	}
	got = (ListFilter{}).Normalized()
	if got.Page != 1 || got.PageSize != DefaultPageSize {
		t.Fatalf("default Normalized() = %+v", got)
	}
}

func TestValidateSubmissionForCreate(t *testing.T) {
	now := time.Now().UTC()
	valid := Submission{
		UserID: 1, ProblemID: 2, Language: "cpp", SourceObjectKey: "sources/id/source.cpp",
		SourceSHA256: strings.Repeat("a", 64), SourceSizeBytes: 10,
		JudgeRevision: testRevision, JudgeDeadlineAt: now.Add(time.Minute),
	}
	if err := ValidateSubmissionForCreate(valid); err != nil {
		t.Fatalf("ValidateSubmissionForCreate() error = %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*Submission)
	}{
		{"actor", func(s *Submission) { s.UserID = 0 }},
		{"language", func(s *Submission) { s.Language = "rust" }},
		{"key", func(s *Submission) { s.SourceObjectKey = "" }},
		{"hash", func(s *Submission) { s.SourceSHA256 = strings.Repeat("A", 64) }},
		{"size", func(s *Submission) { s.SourceSizeBytes = 0 }},
		{"revision", func(s *Submission) { s.JudgeRevision = strings.Repeat("x", 26) }},
		{"deadline", func(s *Submission) { s.JudgeDeadlineAt = time.Time{} }},
		{"retries", func(s *Submission) { s.RetryCount = MaxRetryCount + 1 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := valid
			test.mutate(&input)
			if err := ValidateSubmissionForCreate(input); err == nil {
				t.Fatal("ValidateSubmissionForCreate() error = nil")
			}
		})
	}
}

func TestValidateIdempotency(t *testing.T) {
	now := time.Now().UTC()
	request := IdempotencyRequest{
		ActorID: 1, Operation: OperationCreateSubmission,
		Key:         "123e4567-e89b-12d3-a456-426614174000",
		RequestHash: strings.Repeat("b", 64), ExpiresAt: now.Add(time.Hour),
	}
	if err := ValidateIdempotency(request, OperationCreateSubmission, now); err != nil {
		t.Fatalf("ValidateIdempotency() error = %v", err)
	}
	request.RequestHash = strings.Repeat("B", 64)
	if err := ValidateIdempotency(request, OperationCreateSubmission, now); err == nil {
		t.Fatal("uppercase request hash was accepted")
	}
}

func TestSubmissionUsecaseCreate(t *testing.T) {
	now := time.Date(2026, 9, 20, 1, 2, 3, 0, time.UTC)
	repository := &usecaseRepository{createResult: CreateSubmissionResult{SubmissionID: 41, Status: submissionv1.SubmissionStatus_SUBMISSION_STATUS_QUEUED}}
	sources := &usecaseSourceStore{object: SourceObject{Key: "sources/id/source.go", SHA256: strings.Repeat("a", 64), Size: 13}}
	problems := &usecaseProblemCatalog{profile: JudgeProfile{ProblemID: 7, TimeLimitMS: 1000, MemoryLimitKB: 65536, JudgeRevision: testRevision}}
	uc := NewSubmissionUsecase(repository, sources, problems)
	uc.now = func() time.Time { return now }
	uc.newEventID = func() string { return "123e4567-e89b-12d3-a456-426614174001" }

	result, err := uc.Create(context.Background(), CreateSubmissionInput{
		Actor: Actor{ID: 5, Roles: []string{"user"}}, ProblemID: 7, Language: " Go ",
		SourceCode: []byte("package main\n"), IdempotencyKey: "123e4567-e89b-12d3-a456-426614174000",
	})
	if err != nil || result.SubmissionID != 41 {
		t.Fatalf("Create() = %+v, %v", result, err)
	}
	command := repository.created
	if command.Submission.UserID != 5 || command.Submission.Language != "go" || command.Submission.JudgeRevision != testRevision || command.Submission.JudgeDeadlineAt != now.Add(JudgeQueueDeadline) {
		t.Fatalf("created command = %+v", command)
	}
	if command.Idempotency.RequestHash == "" || command.Idempotency.ExpiresAt != now.Add(IdempotencyTTL) || command.OutboxEventID == "" {
		t.Fatalf("transaction metadata = %+v", command)
	}
	if sources.calls != 1 || problems.calls != 1 {
		t.Fatalf("dependency calls sources=%d problems=%d", sources.calls, problems.calls)
	}
}

func TestSubmissionUsecaseCreateIdempotencyShortCircuitsDependencies(t *testing.T) {
	now := time.Date(2026, 9, 20, 1, 2, 3, 0, time.UTC)
	input := CreateSubmissionInput{
		Actor: Actor{ID: 5}, ProblemID: 7, Language: "go", SourceCode: []byte("package main\n"),
		IdempotencyKey: "123e4567-e89b-12d3-a456-426614174000",
	}
	response, _ := json.Marshal(CreateSubmissionResult{SubmissionID: 41, Status: submissionv1.SubmissionStatus_SUBMISSION_STATUS_QUEUED})
	repository := &usecaseRepository{found: true, record: IdempotencyRecord{
		IdempotencyRequest: IdempotencyRequest{RequestHash: createRequestHash(input.ProblemID, input.Language, input.SourceCode)},
		Response:           response,
	}}
	sources := &usecaseSourceStore{}
	problems := &usecaseProblemCatalog{}
	uc := NewSubmissionUsecase(repository, sources, problems)
	uc.now = func() time.Time { return now }

	result, err := uc.Create(context.Background(), input)
	if err != nil || result.SubmissionID != 41 || !result.Replayed {
		t.Fatalf("Create() replay = %+v, %v", result, err)
	}
	if sources.calls != 0 || problems.calls != 0 || repository.createCalls != 0 {
		t.Fatalf("replay reached dependencies: sources=%d problems=%d creates=%d", sources.calls, problems.calls, repository.createCalls)
	}

	repository.record.RequestHash = strings.Repeat("f", 64)
	if _, err = uc.Create(context.Background(), input); !HasReason(err, ReasonIdempotencyConflict) {
		t.Fatalf("Create() conflict error = %v", err)
	}
}

func TestSubmissionUsecaseGetAuthorization(t *testing.T) {
	repository := &usecaseRepository{submission: Submission{ID: 11, UserID: 5}}
	uc := NewSubmissionUsecase(repository, nil, nil)
	if _, err := uc.Get(context.Background(), Actor{ID: 5}, 11); err != nil {
		t.Fatalf("owner Get() error = %v", err)
	}
	if _, err := uc.Get(context.Background(), Actor{ID: 6}, 11); !HasReason(err, ReasonSubmissionNotFound) {
		t.Fatalf("cross-user Get() error = %v", err)
	}
	if _, err := uc.Get(context.Background(), Actor{ID: 6, Roles: []string{"ADMIN"}}, 11); err != nil {
		t.Fatalf("admin Get() error = %v", err)
	}
}

func TestSubmissionUsecaseListAuthorizationAndNormalization(t *testing.T) {
	repository := &usecaseRepository{page: SubmissionPage{Total: 1}}
	uc := NewSubmissionUsecase(repository, nil, nil)
	page, err := uc.List(context.Background(), Actor{ID: 5}, ListFilter{Language: " Go ", PageSize: 1000})
	if err != nil || page.Total != 1 {
		t.Fatalf("List() = %+v, %v", page, err)
	}
	if repository.listed.UserID != 5 || repository.listed.Language != "go" || repository.listed.Page != 1 || repository.listed.PageSize != MaxPageSize {
		t.Fatalf("owner filter = %+v", repository.listed)
	}
	if _, err = uc.List(context.Background(), Actor{ID: 5}, ListFilter{UserID: 6}); !HasReason(err, ReasonPermissionDenied) {
		t.Fatalf("cross-user List() error = %v", err)
	}
	if _, err = uc.List(context.Background(), Actor{ID: 9, Roles: []string{"admin"}}, ListFilter{}); err != nil || repository.listed.UserID != 0 {
		t.Fatalf("admin filter = %+v, %v", repository.listed, err)
	}
}

type usecaseRepository struct {
	submission   Submission
	page         SubmissionPage
	record       IdempotencyRecord
	found        bool
	created      CreateSubmissionCommand
	createResult CreateSubmissionResult
	createCalls  int
	listed       ListFilter
}

func (r *usecaseRepository) FindByID(context.Context, int64) (Submission, error) {
	return r.submission, nil
}
func (r *usecaseRepository) List(_ context.Context, filter ListFilter) (SubmissionPage, error) {
	r.listed = filter
	return r.page, nil
}
func (r *usecaseRepository) GetJudgeResult(context.Context, int64) (JudgeResult, error) {
	return JudgeResult{}, nil
}
func (r *usecaseRepository) FindIdempotency(context.Context, int64, string, string) (IdempotencyRecord, bool, error) {
	return r.record, r.found, nil
}
func (r *usecaseRepository) CreateWithOutboxAndIdempotency(_ context.Context, command CreateSubmissionCommand) (CreateSubmissionResult, error) {
	r.created = command
	r.createCalls++
	return r.createResult, nil
}
func (r *usecaseRepository) InvalidateAndRequeueWithOutboxAndIdempotency(context.Context, RejudgeSubmissionCommand) (RejudgeSubmissionResult, error) {
	return RejudgeSubmissionResult{}, nil
}

type usecaseSourceStore struct {
	object SourceObject
	calls  int
}

func (s *usecaseSourceStore) Put(context.Context, string, []byte) (SourceObject, error) {
	s.calls++
	return s.object, nil
}

type usecaseProblemCatalog struct {
	profile JudgeProfile
	calls   int
}

func (c *usecaseProblemCatalog) GetJudgeProfile(context.Context, int64) (JudgeProfile, error) {
	c.calls++
	return c.profile, nil
}

package service

import (
	"context"
	"strings"
	"testing"
	"time"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	submissionv1 "github.com/viggggil/go_oj_agent/api/submission/v1"
	"github.com/viggggil/go_oj_agent/pkg/internalauth"
	"github.com/viggggil/go_oj_agent/services/judge/internal/biz"
)

const serviceTestRevision = "01K5C6Y7N8P9Q0R1S2T3V4W5X6"

func TestSubmissionServiceCreateGetAndList(t *testing.T) {
	now := time.Date(2026, 9, 20, 2, 3, 4, 0, time.UTC)
	repository := &serviceRepository{
		createResult: biz.CreateSubmissionResult{SubmissionID: 12, Status: submissionv1.SubmissionStatus_SUBMISSION_STATUS_QUEUED},
		submission:   biz.Submission{ID: 12, UserID: 5, ProblemID: 7, Language: "go", Status: submissionv1.SubmissionStatus_SUBMISSION_STATUS_QUEUED, JudgeRevision: serviceTestRevision, CreatedAt: now, UpdatedAt: now},
	}
	repository.page = biz.SubmissionPage{Items: []biz.Submission{repository.submission}, Page: 1, PageSize: 20, Total: 1}
	uc := biz.NewSubmissionUsecase(repository, serviceSourceStore{}, serviceProblemCatalog{})
	service := NewSubmissionService(uc)
	ctx := internalauth.WithPrincipal(context.Background(), internalauth.Principal{Caller: "gateway-service", ActorID: 5, ActorRoles: []string{"user"}})

	created, err := service.CreateSubmission(ctx, &submissionv1.CreateSubmissionRequest{
		ProblemId: 7, Language: "go", SourceCode: "package main\n", IdempotencyKey: "123e4567-e89b-12d3-a456-426614174000",
	})
	if err != nil || created.GetSubmissionId() != 12 || created.GetStatus() != submissionv1.SubmissionStatus_SUBMISSION_STATUS_QUEUED {
		t.Fatalf("CreateSubmission() = %+v, %v", created, err)
	}
	got, err := service.GetSubmission(ctx, &submissionv1.GetSubmissionRequest{SubmissionId: 12})
	if err != nil || got.GetSubmission().GetId() != 12 || got.GetSubmission().GetCreatedAt() == nil {
		t.Fatalf("GetSubmission() = %+v, %v", got, err)
	}
	listed, err := service.ListSubmissions(ctx, &submissionv1.ListSubmissionsRequest{Page: &commonv1.PageRequest{Page: 1, PageSize: 20}})
	if err != nil || len(listed.GetItems()) != 1 || listed.GetPage().GetTotal() != 1 {
		t.Fatalf("ListSubmissions() = %+v, %v", listed, err)
	}
}

func TestSubmissionServiceRejectsInvalidRequestAndMissingPrincipal(t *testing.T) {
	service := NewSubmissionService(biz.NewSubmissionUsecase(&serviceRepository{}, serviceSourceStore{}, serviceProblemCatalog{}))
	if _, err := service.CreateSubmission(context.Background(), &submissionv1.CreateSubmissionRequest{}); !biz.HasReason(err, biz.ReasonInvalidArgument) {
		t.Fatalf("invalid request error = %v", err)
	}
	request := &submissionv1.GetSubmissionRequest{SubmissionId: 1}
	if _, err := service.GetSubmission(context.Background(), request); !biz.HasReason(err, biz.ReasonUnauthenticated) {
		t.Fatalf("missing principal error = %v", err)
	}
}

type serviceRepository struct {
	createResult biz.CreateSubmissionResult
	submission   biz.Submission
	page         biz.SubmissionPage
}

func (r *serviceRepository) FindByID(context.Context, int64) (biz.Submission, error) {
	return r.submission, nil
}
func (r *serviceRepository) List(context.Context, biz.ListFilter) (biz.SubmissionPage, error) {
	return r.page, nil
}
func (r *serviceRepository) GetJudgeResult(context.Context, int64) (biz.JudgeResult, error) {
	return biz.JudgeResult{}, nil
}
func (r *serviceRepository) FindIdempotency(context.Context, int64, string, string) (biz.IdempotencyRecord, bool, error) {
	return biz.IdempotencyRecord{}, false, nil
}
func (r *serviceRepository) CreateWithOutboxAndIdempotency(context.Context, biz.CreateSubmissionCommand) (biz.CreateSubmissionResult, error) {
	return r.createResult, nil
}
func (r *serviceRepository) InvalidateAndRequeueWithOutboxAndIdempotency(context.Context, biz.RejudgeSubmissionCommand) (biz.RejudgeSubmissionResult, error) {
	return biz.RejudgeSubmissionResult{}, nil
}

type serviceSourceStore struct{}

func (serviceSourceStore) Put(context.Context, string, []byte) (biz.SourceObject, error) {
	return biz.SourceObject{Key: "sources/id/source.go", SHA256: strings.Repeat("a", 64), Size: 13}, nil
}

type serviceProblemCatalog struct{}

func (serviceProblemCatalog) GetJudgeProfile(context.Context, int64) (biz.JudgeProfile, error) {
	return biz.JudgeProfile{ProblemID: 7, TimeLimitMS: 1000, MemoryLimitKB: 65536, JudgeRevision: serviceTestRevision}, nil
}

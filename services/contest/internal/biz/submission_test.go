package biz

import (
	"context"
	"testing"
	"time"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	contestv1 "github.com/viggggil/go_oj_agent/api/contest/v1"
	submissionv1 "github.com/viggggil/go_oj_agent/api/submission/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type contestSubmissionRepo struct {
	fakeContestRepo
	participant bool
	problem     bool
}

func (r *contestSubmissionRepo) IsParticipant(context.Context, int64, int64) (bool, error) {
	return r.participant, nil
}
func (r *contestSubmissionRepo) HasProblem(context.Context, int64, int64) (bool, error) {
	return r.problem, nil
}

type submissionCreatorFake struct {
	request *submissionv1.CreateSubmissionRequest
}

func (f *submissionCreatorFake) CreateSubmission(_ context.Context, req *submissionv1.CreateSubmissionRequest, _ ...grpc.CallOption) (*submissionv1.CreateSubmissionResponse, error) {
	f.request = req
	return &submissionv1.CreateSubmissionResponse{SubmissionId: 1001, Status: submissionv1.SubmissionStatus_SUBMISSION_STATUS_QUEUED}, nil
}

func TestCreateContestSubmissionValidatesContextAndForwardsContestID(t *testing.T) {
	now := time.Now().UTC()
	repo := &contestSubmissionRepo{
		fakeContestRepo: fakeContestRepo{contest: Contest{ID: 20, StartAt: now.Add(-time.Minute), EndAt: now.Add(time.Minute), Status: contestv1.ContestStatus_CONTEST_STATUS_DRAFT}},
		participant:     true, problem: true,
	}
	creator := &submissionCreatorFake{}
	uc := NewContestUsecaseWithRepositoryAndSubmission(repo, creator)
	uc.now = func() time.Time { return now }
	result, err := uc.CreateSubmission(context.Background(), &commonRequestUser42, &contestv1.CreateContestSubmissionRequest{ContestId: 20, ProblemId: 7, Language: "go", SourceCode: "package main", IdempotencyKey: "550e8400-e29b-41d4-a716-446655440000"})
	if err != nil || result.GetSubmissionId() != 1001 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if creator.request.GetContestId() != 20 || creator.request.GetProblemId() != 7 {
		t.Fatalf("forwarded request=%+v", creator.request)
	}
}

var commonRequestUser42 = commonv1.RequestContext{UserId: 42}

func TestCreateContestSubmissionRejectsNonParticipant(t *testing.T) {
	now := time.Now().UTC()
	repo := &contestSubmissionRepo{fakeContestRepo: fakeContestRepo{contest: Contest{ID: 20, StartAt: now.Add(-time.Minute), EndAt: now.Add(time.Minute), Status: contestv1.ContestStatus_CONTEST_STATUS_DRAFT}}}
	uc := NewContestUsecaseWithRepositoryAndSubmission(repo, &submissionCreatorFake{})
	uc.now = func() time.Time { return now }
	_, err := uc.CreateSubmission(context.Background(), &commonRequestUser42, &contestv1.CreateContestSubmissionRequest{ContestId: 20, ProblemId: 7})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("code=%v err=%v", status.Code(err), err)
	}
}

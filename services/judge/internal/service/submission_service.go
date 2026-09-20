package service

import (
	"context"
	"strings"

	"github.com/google/wire"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	submissionv1 "github.com/viggggil/go_oj_agent/api/submission/v1"
	"github.com/viggggil/go_oj_agent/pkg/internalauth"
	"github.com/viggggil/go_oj_agent/services/judge/internal/biz"
)

const Name = "judge-service"

var ProviderSet = wire.NewSet(NewSubmissionService)

// SubmissionService registers the public contract while business slices are
// implemented incrementally. Embedded handlers return codes.Unimplemented.
type SubmissionService struct {
	submissionv1.UnimplementedSubmissionServiceServer
	uc *biz.SubmissionUsecase
}

func NewSubmissionService(uc *biz.SubmissionUsecase) *SubmissionService {
	return &SubmissionService{uc: uc}
}

func (s *SubmissionService) CreateSubmission(ctx context.Context, req *submissionv1.CreateSubmissionRequest) (*submissionv1.CreateSubmissionResponse, error) {
	if req == nil || s == nil || s.uc == nil {
		return nil, biz.ErrorInvalidArgument("invalid create submission request")
	}
	if err := req.Validate(); err != nil {
		return nil, biz.ErrorInvalidArgument("%s", err.Error())
	}
	actor, err := actorFromPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	result, err := s.uc.Create(ctx, biz.CreateSubmissionInput{
		Actor: actor, ProblemID: req.GetProblemId(), Language: req.GetLanguage(),
		SourceCode: []byte(req.GetSourceCode()), IdempotencyKey: strings.ToLower(req.GetIdempotencyKey()),
	})
	if err != nil {
		return nil, err
	}
	return &submissionv1.CreateSubmissionResponse{SubmissionId: result.SubmissionID, Status: result.Status}, nil
}

func (s *SubmissionService) GetSubmission(ctx context.Context, req *submissionv1.GetSubmissionRequest) (*submissionv1.GetSubmissionResponse, error) {
	if req == nil || s == nil || s.uc == nil {
		return nil, biz.ErrorInvalidArgument("invalid get submission request")
	}
	if err := req.Validate(); err != nil {
		return nil, biz.ErrorInvalidArgument("%s", err.Error())
	}
	actor, err := actorFromPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	submission, err := s.uc.Get(ctx, actor, req.GetSubmissionId())
	if err != nil {
		return nil, err
	}
	return &submissionv1.GetSubmissionResponse{Submission: toProtoSubmission(submission)}, nil
}

func (s *SubmissionService) ListSubmissions(ctx context.Context, req *submissionv1.ListSubmissionsRequest) (*submissionv1.ListSubmissionsResponse, error) {
	if req == nil || s == nil || s.uc == nil {
		return nil, biz.ErrorInvalidArgument("invalid list submissions request")
	}
	if err := req.Validate(); err != nil {
		return nil, biz.ErrorInvalidArgument("%s", err.Error())
	}
	actor, err := actorFromPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	page, err := s.uc.List(ctx, actor, biz.ListFilter{
		UserID: req.GetUserId(), ProblemID: req.GetProblemId(), Status: req.GetStatus(), Language: req.GetLanguage(),
		Page: req.GetPage().GetPage(), PageSize: req.GetPage().GetPageSize(),
	})
	if err != nil {
		return nil, err
	}
	items := make([]*submissionv1.Submission, 0, len(page.Items))
	for _, submission := range page.Items {
		items = append(items, toProtoSubmission(submission))
	}
	return &submissionv1.ListSubmissionsResponse{
		Items: items,
		Page:  &commonv1.PageResponse{Page: page.Page, PageSize: page.PageSize, Total: page.Total},
	}, nil
}

func actorFromPrincipal(ctx context.Context) (biz.Actor, error) {
	principal, ok := internalauth.PrincipalFromContext(ctx)
	if !ok || principal.ActorID <= 0 {
		return biz.Actor{}, biz.ErrorUnauthenticated("trusted principal is missing")
	}
	return biz.Actor{
		ID: principal.ActorID, Roles: append([]string(nil), principal.ActorRoles...),
		RequestID: principal.RequestID, TraceID: principal.TraceID,
	}, nil
}

func toProtoSubmission(submission biz.Submission) *submissionv1.Submission {
	result := &submissionv1.Submission{
		Id: submission.ID, UserId: submission.UserID, ProblemId: submission.ProblemID,
		Language: submission.Language, Status: submission.Status, Verdict: submission.Verdict,
		JudgeRevision: submission.JudgeRevision, RetryCount: submission.RetryCount,
		SystemErrorReason: submission.SystemErrorReason,
	}
	if submission.TimeMS != nil {
		result.TimeMs = *submission.TimeMS
	}
	if submission.MemoryKB != nil {
		result.MemoryKb = *submission.MemoryKB
	}
	if !submission.CreatedAt.IsZero() {
		result.CreatedAt = timestamppb.New(submission.CreatedAt)
	}
	if !submission.UpdatedAt.IsZero() {
		result.UpdatedAt = timestamppb.New(submission.UpdatedAt)
	}
	if submission.JudgedAt != nil {
		result.JudgedAt = timestamppb.New(*submission.JudgedAt)
	}
	if submission.InvalidatedAt != nil {
		result.InvalidatedAt = timestamppb.New(*submission.InvalidatedAt)
	}
	return result
}

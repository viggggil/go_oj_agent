package biz

import (
	"context"
	"strings"
	"time"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	contestv1 "github.com/viggggil/go_oj_agent/api/contest/v1"
	submissionv1 "github.com/viggggil/go_oj_agent/api/submission/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const RoleAdmin = "admin"

type Contest struct {
	ID                   int64
	Title                string
	Status               contestv1.ContestStatus
	StartAt, EndAt       time.Time
	CreatedBy            int64
	CreatedAt, UpdatedAt time.Time
	Problems             []ContestProblem
}
type ContestProblem struct {
	ProblemID        int64
	SortOrder, Score int32
}

type ContestRepository interface {
	Create(context.Context, Contest) (Contest, error)
	Get(context.Context, int64) (Contest, error)
	List(context.Context, int32, int32, contestv1.ContestStatus) ([]Contest, int64, error)
	Update(context.Context, Contest) (Contest, error)
	Archive(context.Context, int64) (Contest, error)
}

type ContestSubmissionRepository interface {
	ContestRepository
	IsParticipant(context.Context, int64, int64) (bool, error)
	HasProblem(context.Context, int64, int64) (bool, error)
}

type SubmissionCreator interface {
	CreateSubmission(context.Context, *submissionv1.CreateSubmissionRequest, ...grpc.CallOption) (*submissionv1.CreateSubmissionResponse, error)
}

type ContestUsecase struct {
	repo       ContestRepository
	submission SubmissionCreator
	now        func() time.Time
}

func NewContestUsecase() *ContestUsecase {
	return &ContestUsecase{now: func() time.Time { return time.Now().UTC() }}
}
func NewContestUsecaseWithRepository(repo ContestRepository) *ContestUsecase {
	return &ContestUsecase{repo: repo, now: func() time.Time { return time.Now().UTC() }}
}

func NewContestUsecaseWithRepositoryAndSubmission(repo ContestRepository, submission SubmissionCreator) *ContestUsecase {
	return &ContestUsecase{repo: repo, submission: submission, now: func() time.Time { return time.Now().UTC() }}
}

func (u *ContestUsecase) CreateSubmission(ctx context.Context, actor *commonv1.RequestContext, input *contestv1.CreateContestSubmissionRequest) (*submissionv1.CreateSubmissionResponse, error) {
	if u == nil || u.repo == nil || u.submission == nil {
		return nil, status.Error(codes.Unimplemented, "contest submission is not implemented")
	}
	checker, ok := u.repo.(ContestSubmissionRepository)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "contest submission repository is not implemented")
	}
	if actor == nil || actor.GetUserId() <= 0 {
		return nil, status.Error(codes.Unauthenticated, "authenticated actor is required")
	}
	if input == nil || input.GetContestId() <= 0 || input.GetProblemId() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "invalid contest submission")
	}
	contest, err := u.repo.Get(ctx, input.GetContestId())
	if err != nil {
		return nil, err
	}
	now := u.now()
	if contest.Status == contestv1.ContestStatus_CONTEST_STATUS_ARCHIVED || now.Before(contest.StartAt) || !now.Before(contest.EndAt) || lifecycle(contest.StartAt, contest.EndAt, contest.Status, now) != contestv1.ContestStatus_CONTEST_STATUS_RUNNING {
		return nil, status.Error(codes.FailedPrecondition, "contest is not accepting submissions")
	}
	participant, err := checker.IsParticipant(ctx, input.GetContestId(), actor.GetUserId())
	if err != nil {
		return nil, err
	}
	if !participant {
		return nil, status.Error(codes.PermissionDenied, "user is not a contest participant")
	}
	problem, err := checker.HasProblem(ctx, input.GetContestId(), input.GetProblemId())
	if err != nil {
		return nil, err
	}
	if !problem {
		return nil, status.Error(codes.InvalidArgument, "problem does not belong to contest")
	}
	return u.submission.CreateSubmission(ctx, &submissionv1.CreateSubmissionRequest{
		ProblemId: input.GetProblemId(), ContestId: input.GetContestId(), Language: input.GetLanguage(),
		SourceCode: input.GetSourceCode(), IdempotencyKey: input.GetIdempotencyKey(),
	})
}

func (u *ContestUsecase) Create(ctx context.Context, actor *commonv1.RequestContext, input *contestv1.ContestUpdate) (Contest, error) {
	if u == nil || u.repo == nil {
		return Contest{}, status.Error(codes.Unimplemented, "contest data is not implemented")
	}
	if err := requireAdmin(actor); err != nil {
		return Contest{}, err
	}
	start, end, problems, err := normalizeUpdate(input)
	if err != nil {
		return Contest{}, err
	}
	if !start.After(u.now()) {
		return Contest{}, status.Error(codes.InvalidArgument, "contest start_at must be in the future")
	}
	return u.repo.Create(ctx, Contest{Title: strings.TrimSpace(input.GetTitle()), Status: contestv1.ContestStatus_CONTEST_STATUS_DRAFT, StartAt: start, EndAt: end, CreatedBy: actor.GetUserId(), Problems: problems})
}

func (u *ContestUsecase) Get(ctx context.Context, actor *commonv1.RequestContext, id int64) (Contest, error) {
	if u == nil || u.repo == nil {
		return Contest{}, status.Error(codes.Unimplemented, "contest data is not implemented")
	}
	if actor == nil || actor.GetUserId() <= 0 || id <= 0 {
		return Contest{}, status.Error(codes.InvalidArgument, "invalid contest request")
	}
	contest, err := u.repo.Get(ctx, id)
	if err != nil {
		return Contest{}, err
	}
	contest.Status = lifecycle(contest.StartAt, contest.EndAt, contest.Status, u.now())
	if contest.Status == contestv1.ContestStatus_CONTEST_STATUS_ARCHIVED && !isAdmin(actor) {
		return Contest{}, status.Error(codes.NotFound, "contest not found")
	}
	return contest, nil
}

func (u *ContestUsecase) List(ctx context.Context, actor *commonv1.RequestContext, page, pageSize int32, filter contestv1.ContestStatus) ([]Contest, int64, error) {
	if u == nil || u.repo == nil {
		return nil, 0, status.Error(codes.Unimplemented, "contest data is not implemented")
	}
	if actor == nil || actor.GetUserId() <= 0 {
		return nil, 0, status.Error(codes.Unauthenticated, "authenticated actor is required")
	}
	if page <= 0 || pageSize <= 0 || pageSize > 100 {
		return nil, 0, status.Error(codes.InvalidArgument, "invalid page")
	}
	if !isAdmin(actor) && filter == contestv1.ContestStatus_CONTEST_STATUS_ARCHIVED {
		return nil, 0, status.Error(codes.PermissionDenied, "archived contests are restricted")
	}
	items, total, err := u.repo.List(ctx, page, pageSize, filter)
	if err != nil {
		return nil, 0, err
	}
	for i := range items {
		items[i].Status = lifecycle(items[i].StartAt, items[i].EndAt, items[i].Status, u.now())
	}
	return items, total, nil
}

func (u *ContestUsecase) Update(ctx context.Context, actor *commonv1.RequestContext, id int64, input *contestv1.ContestUpdate) (Contest, error) {
	if u == nil || u.repo == nil {
		return Contest{}, status.Error(codes.Unimplemented, "contest data is not implemented")
	}
	if err := requireAdmin(actor); err != nil {
		return Contest{}, err
	}
	current, err := u.repo.Get(ctx, id)
	if err != nil {
		return Contest{}, err
	}
	current.Status = lifecycle(current.StartAt, current.EndAt, current.Status, u.now())
	if current.Status != contestv1.ContestStatus_CONTEST_STATUS_DRAFT {
		return Contest{}, status.Error(codes.FailedPrecondition, "only draft contests can be updated")
	}
	start, end, problems, err := normalizeUpdate(input)
	if err != nil {
		return Contest{}, err
	}
	if !start.After(u.now()) {
		return Contest{}, status.Error(codes.InvalidArgument, "contest start_at must be in the future")
	}
	current.Title, current.StartAt, current.EndAt, current.Problems = strings.TrimSpace(input.GetTitle()), start, end, problems
	return u.repo.Update(ctx, current)
}

func (u *ContestUsecase) Archive(ctx context.Context, actor *commonv1.RequestContext, id int64) (Contest, error) {
	if u == nil || u.repo == nil {
		return Contest{}, status.Error(codes.Unimplemented, "contest data is not implemented")
	}
	if err := requireAdmin(actor); err != nil {
		return Contest{}, err
	}
	current, err := u.repo.Get(ctx, id)
	if err != nil {
		return Contest{}, err
	}
	if lifecycle(current.StartAt, current.EndAt, current.Status, u.now()) != contestv1.ContestStatus_CONTEST_STATUS_DRAFT {
		return Contest{}, status.Error(codes.FailedPrecondition, "only draft contests can be archived")
	}
	return u.repo.Archive(ctx, id)
}

func normalizeUpdate(input *contestv1.ContestUpdate) (time.Time, time.Time, []ContestProblem, error) {
	if input == nil || strings.TrimSpace(input.GetTitle()) == "" || input.GetStartAt() == nil || input.GetEndAt() == nil {
		return time.Time{}, time.Time{}, nil, status.Error(codes.InvalidArgument, "complete contest fields are required")
	}
	start, end := input.GetStartAt().AsTime(), input.GetEndAt().AsTime()
	if !start.Before(end) {
		return time.Time{}, time.Time{}, nil, status.Error(codes.InvalidArgument, "start_at must be before end_at")
	}
	problems, seen := make([]ContestProblem, 0, len(input.GetProblems())), make(map[int64]struct{}, len(input.GetProblems()))
	for _, problem := range input.GetProblems() {
		if problem == nil || problem.GetProblemId() <= 0 || problem.GetSortOrder() <= 0 || problem.GetScore() < 0 {
			return time.Time{}, time.Time{}, nil, status.Error(codes.InvalidArgument, "invalid contest problem")
		}
		if _, ok := seen[problem.GetProblemId()]; ok {
			return time.Time{}, time.Time{}, nil, status.Error(codes.InvalidArgument, "duplicate contest problem")
		}
		seen[problem.GetProblemId()] = struct{}{}
		problems = append(problems, ContestProblem{ProblemID: problem.GetProblemId(), SortOrder: problem.GetSortOrder(), Score: problem.GetScore()})
	}
	if len(problems) == 0 {
		return time.Time{}, time.Time{}, nil, status.Error(codes.InvalidArgument, "contest must contain at least one problem")
	}
	return start, end, problems, nil
}

func lifecycle(start, end time.Time, stored contestv1.ContestStatus, now time.Time) contestv1.ContestStatus {
	if stored == contestv1.ContestStatus_CONTEST_STATUS_ARCHIVED {
		return stored
	}
	if !now.Before(end) {
		return contestv1.ContestStatus_CONTEST_STATUS_ENDED
	}
	if !now.Before(start) {
		return contestv1.ContestStatus_CONTEST_STATUS_RUNNING
	}
	return contestv1.ContestStatus_CONTEST_STATUS_DRAFT
}
func requireAdmin(actor *commonv1.RequestContext) error {
	if actor == nil || actor.GetUserId() <= 0 {
		return status.Error(codes.Unauthenticated, "authenticated actor is required")
	}
	if !isAdmin(actor) {
		return status.Error(codes.PermissionDenied, "admin role required")
	}
	return nil
}
func isAdmin(actor *commonv1.RequestContext) bool {
	if actor == nil {
		return false
	}
	for _, role := range actor.GetRoles() {
		if strings.EqualFold(strings.TrimSpace(role), RoleAdmin) {
			return true
		}
	}
	return false
}

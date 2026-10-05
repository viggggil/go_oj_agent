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
	"google.golang.org/protobuf/types/known/timestamppb"
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
	Joined               bool
}
type ContestProblem struct {
	ProblemID        int64
	SortOrder, Score int32
	Title            string
}

type ContestRepository interface {
	Create(context.Context, Contest) (Contest, error)
	Get(context.Context, int64) (Contest, error)
	List(context.Context, int32, int32, contestv1.ContestStatus) ([]Contest, int64, error)
	Update(context.Context, Contest) (Contest, error)
	Archive(context.Context, int64) (Contest, error)
}

// 生产 Repository 提供数据库 UTC 时钟；纯业务测试可继续注入 now。
type ContestClock interface {
	CurrentTime(context.Context) (time.Time, error)
}

func (u *ContestUsecase) currentTime(ctx context.Context) (time.Time, error) {
	if clock, ok := u.repo.(ContestClock); ok {
		return clock.CurrentTime(ctx)
	}
	return u.now().UTC(), nil
}

type ContestSubmissionRepository interface {
	ContestRepository
	IsParticipant(context.Context, int64, int64) (bool, error)
	HasProblem(context.Context, int64, int64) (bool, error)
	Join(context.Context, int64, int64) (time.Time, error)
}

// 比赛提交预校验必须在共享比赛锁内完成，防止跨开始边界的配置事务交错。
type ContestSubmissionAuthorizer interface {
	AuthorizeSubmission(context.Context, int64, int64, int64) (Contest, error)
}

type SubmissionCreator interface {
	CreateSubmission(context.Context, *submissionv1.CreateSubmissionRequest, ...grpc.CallOption) (*submissionv1.CreateSubmissionResponse, error)
}

type ContestUsecase struct {
	repo       ContestRepository
	submission SubmissionCreator
	problems   ProblemCatalog
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
	contest, err := u.authorizeSubmission(ctx, checker, input.GetContestId(), actor.GetUserId(), input.GetProblemId())
	if err != nil {
		return nil, err
	}
	return u.submission.CreateSubmission(ctx, &submissionv1.CreateSubmissionRequest{
		ProblemId: input.GetProblemId(), ContestId: input.GetContestId(), Language: input.GetLanguage(),
		SourceCode: input.GetSourceCode(), IdempotencyKey: input.GetIdempotencyKey(),
		ContestStartAt: timestamppb.New(contest.StartAt), ContestEndAt: timestamppb.New(contest.EndAt),
	})
}

func (u *ContestUsecase) authorizeSubmission(ctx context.Context, checker ContestSubmissionRepository, contestID, userID, problemID int64) (Contest, error) {
	if authorizer, ok := checker.(ContestSubmissionAuthorizer); ok {
		return authorizer.AuthorizeSubmission(ctx, contestID, userID, problemID)
	}
	contest, err := u.repo.Get(ctx, contestID)
	if err != nil {
		return Contest{}, err
	}
	now, err := u.currentTime(ctx)
	if err != nil {
		return Contest{}, err
	}
	if contest.Status == contestv1.ContestStatus_CONTEST_STATUS_ARCHIVED || now.Before(contest.StartAt) || !now.Before(contest.EndAt) || lifecycle(contest.StartAt, contest.EndAt, contest.Status, now) != contestv1.ContestStatus_CONTEST_STATUS_RUNNING {
		return Contest{}, status.Error(codes.FailedPrecondition, "contest is not accepting submissions")
	}
	participant, err := checker.IsParticipant(ctx, contestID, userID)
	if err != nil {
		return Contest{}, err
	}
	if !participant {
		return Contest{}, status.Error(codes.PermissionDenied, "user is not a contest participant")
	}
	problem, err := checker.HasProblem(ctx, contestID, problemID)
	if err != nil {
		return Contest{}, err
	}
	if !problem {
		return Contest{}, status.Error(codes.InvalidArgument, "problem does not belong to contest")
	}
	return contest, nil
}

func (u *ContestUsecase) Join(ctx context.Context, actor *commonv1.RequestContext, contestID int64) (time.Time, error) {
	if u == nil || u.repo == nil {
		return time.Time{}, status.Error(codes.Unimplemented, "contest data is not implemented")
	}
	checker, ok := u.repo.(ContestSubmissionRepository)
	if !ok {
		return time.Time{}, status.Error(codes.Unimplemented, "contest participant repository is not implemented")
	}
	if actor == nil || actor.GetUserId() <= 0 {
		return time.Time{}, status.Error(codes.Unauthenticated, "authenticated actor is required")
	}
	if contestID <= 0 {
		return time.Time{}, status.Error(codes.InvalidArgument, "invalid contest id")
	}
	// Keep a fast user-facing check; the repository repeats it under the
	// contest row lock so this read cannot create a race.
	contest, err := u.repo.Get(ctx, contestID)
	if err != nil {
		return time.Time{}, err
	}
	now, err := u.currentTime(ctx)
	if err != nil {
		return time.Time{}, err
	}
	if contest.Status == contestv1.ContestStatus_CONTEST_STATUS_ARCHIVED || !now.Before(contest.StartAt) || !now.Before(contest.EndAt) || lifecycle(contest.StartAt, contest.EndAt, contest.Status, now) != contestv1.ContestStatus_CONTEST_STATUS_DRAFT {
		return time.Time{}, status.Error(codes.FailedPrecondition, "contest is not accepting registrations")
	}
	return checker.Join(ctx, contestID, actor.GetUserId())
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
	now, err := u.currentTime(ctx)
	if err != nil {
		return Contest{}, err
	}
	if !start.After(now) {
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
	now, err := u.currentTime(ctx)
	if err != nil {
		return Contest{}, err
	}
	contest.Status = lifecycle(contest.StartAt, contest.EndAt, contest.Status, now)
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
	now, err := u.currentTime(ctx)
	if err != nil {
		return nil, 0, err
	}
	for i := range items {
		items[i].Status = lifecycle(items[i].StartAt, items[i].EndAt, items[i].Status, now)
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
	now, err := u.currentTime(ctx)
	if err != nil {
		return Contest{}, err
	}
	current.Status = lifecycle(current.StartAt, current.EndAt, current.Status, now)
	if current.Status != contestv1.ContestStatus_CONTEST_STATUS_DRAFT {
		return Contest{}, status.Error(codes.FailedPrecondition, "only draft contests can be updated")
	}
	start, end, problems, err := normalizeUpdate(input)
	if err != nil {
		return Contest{}, err
	}
	if !start.After(now) {
		return Contest{}, status.Error(codes.InvalidArgument, "contest start_at must be in the future")
	}
	if input.GetExpectedUpdatedAt() == nil {
		return Contest{}, status.Error(codes.InvalidArgument, "contest expected_updated_at is required")
	}
	if err := input.GetExpectedUpdatedAt().CheckValid(); err != nil {
		return Contest{}, status.Error(codes.InvalidArgument, "invalid contest expected_updated_at")
	}
	current.UpdatedAt = input.GetExpectedUpdatedAt().AsTime().UTC()
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
	// The repository performs the authoritative check while holding the row
	// lock; this read only preserves the existing fast validation semantics.
	current, err := u.repo.Get(ctx, id)
	if err != nil {
		return Contest{}, err
	}
	now, err := u.currentTime(ctx)
	if err != nil {
		return Contest{}, err
	}
	if lifecycle(current.StartAt, current.EndAt, current.Status, now) != contestv1.ContestStatus_CONTEST_STATUS_DRAFT {
		return Contest{}, status.Error(codes.FailedPrecondition, "only draft contests can be archived")
	}
	return u.repo.Archive(ctx, id)
}

func normalizeUpdate(input *contestv1.ContestUpdate) (time.Time, time.Time, []ContestProblem, error) {
	if input == nil || strings.TrimSpace(input.GetTitle()) == "" || input.GetStartAt() == nil || input.GetEndAt() == nil {
		return time.Time{}, time.Time{}, nil, status.Error(codes.InvalidArgument, "complete contest fields are required")
	}
	// Contest 的存储精度为 DATETIME(3)，预校验必须使用相同精度。
	start, end := input.GetStartAt().AsTime().UTC().Truncate(time.Millisecond), input.GetEndAt().AsTime().UTC().Truncate(time.Millisecond)
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

type ProblemCatalog interface {
	Titles(context.Context, []int64) (map[int64]string, error)
}

func NewContestUsecaseWithClients(repo ContestRepository, submission SubmissionCreator, problems ProblemCatalog) *ContestUsecase {
	u := NewContestUsecaseWithRepositoryAndSubmission(repo, submission)
	u.problems = problems
	return u
}
func (u *ContestUsecase) GetDetails(ctx context.Context, actor *commonv1.RequestContext, id int64) (Contest, error) {
	c, err := u.Get(ctx, actor, id)
	if err != nil {
		return Contest{}, err
	}
	checker, ok := u.repo.(interface {
		IsParticipant(context.Context, int64, int64) (bool, error)
	})
	if !ok {
		return Contest{}, status.Error(codes.Internal, "participant repository is not configured")
	}
	c.Joined, err = checker.IsParticipant(ctx, id, actor.GetUserId())
	if err != nil {
		return Contest{}, err
	}

	if u.problems == nil {
		return Contest{}, status.Error(codes.Internal, "problem catalog is not configured")
	}
	if len(c.Problems) == 0 {
		return c, nil
	}
	ids := make([]int64, 0, len(c.Problems))
	for _, p := range c.Problems {
		ids = append(ids, p.ProblemID)
	}
	titles, err := u.problems.Titles(ctx, ids)
	if err != nil {
		return Contest{}, err
	}
	for i := range c.Problems {
		c.Problems[i].Title = titles[c.Problems[i].ProblemID]
	}
	return c, nil
}

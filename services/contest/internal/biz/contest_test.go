package biz

import (
	"context"
	"testing"
	"time"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	contestv1 "github.com/viggggil/go_oj_agent/api/contest/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type fakeContestRepo struct {
	contest  Contest
	archived bool
}

func (r *fakeContestRepo) Create(_ context.Context, c Contest) (Contest, error) {
	c.ID = 1
	r.contest = c
	return c, nil
}
func (r *fakeContestRepo) Get(_ context.Context, id int64) (Contest, error) {
	if id != r.contest.ID {
		return Contest{}, status.Error(codes.NotFound, "not found")
	}
	return r.contest, nil
}
func (r *fakeContestRepo) List(context.Context, int32, int32, contestv1.ContestStatus) ([]Contest, int64, error) {
	return []Contest{r.contest}, 1, nil
}
func (r *fakeContestRepo) Update(_ context.Context, c Contest) (Contest, error) {
	r.contest = c
	return c, nil
}
func (r *fakeContestRepo) Archive(_ context.Context, id int64) (Contest, error) {
	r.archived = true
	r.contest.Status = contestv1.ContestStatus_CONTEST_STATUS_ARCHIVED
	return r.contest, nil
}

func contestInput(start time.Time) *contestv1.ContestUpdate {
	return &contestv1.ContestUpdate{Title: "Spring Contest", StartAt: timestamppb.New(start), EndAt: timestamppb.New(start.Add(time.Hour)), Problems: []*contestv1.ContestProblem{{ProblemId: 7, SortOrder: 1, Score: 100}}}
}
func admin() *commonv1.RequestContext {
	return &commonv1.RequestContext{UserId: 9, Roles: []string{"admin"}}
}

func TestCreateUpdateArchiveDraftContest(t *testing.T) {
	repo := &fakeContestRepo{}
	uc := NewContestUsecaseWithRepository(repo)
	created, err := uc.Create(context.Background(), admin(), contestInput(time.Now().Add(time.Hour)))
	if err != nil || created.ID != 1 || created.Status != contestv1.ContestStatus_CONTEST_STATUS_DRAFT {
		t.Fatalf("create = %+v err=%v", created, err)
	}
	updateInput := contestInput(time.Now().Add(2 * time.Hour))
	updateInput.ExpectedUpdatedAt = timestamppb.New(created.UpdatedAt)
	updated, err := uc.Update(context.Background(), admin(), created.ID, updateInput)
	if err != nil || updated.Title != "Spring Contest" {
		t.Fatalf("update = %+v err=%v", updated, err)
	}
	archived, err := uc.Archive(context.Background(), admin(), created.ID)
	if err != nil || archived.Status != contestv1.ContestStatus_CONTEST_STATUS_ARCHIVED || !repo.archived {
		t.Fatalf("archive = %+v err=%v", archived, err)
	}
}

func TestContestUpdateRejectsStartedContestAndDuplicateProblems(t *testing.T) {
	repo := &fakeContestRepo{contest: Contest{ID: 1, StartAt: time.Now().Add(-time.Minute), EndAt: time.Now().Add(time.Hour), Status: contestv1.ContestStatus_CONTEST_STATUS_DRAFT}}
	uc := NewContestUsecaseWithRepository(repo)
	startedInput := contestInput(time.Now().Add(time.Hour))
	startedInput.ExpectedUpdatedAt = timestamppb.New(repo.contest.UpdatedAt)
	if _, err := uc.Update(context.Background(), admin(), 1, startedInput); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("started update code=%v", status.Code(err))
	}
	repo.contest.StartAt = time.Now().Add(time.Hour)
	repo.contest.EndAt = time.Now().Add(2 * time.Hour)
	input := contestInput(time.Now().Add(time.Hour))
	input.ExpectedUpdatedAt = timestamppb.New(repo.contest.UpdatedAt)
	input.Problems = append(input.Problems, input.Problems[0])
	if _, err := uc.Update(context.Background(), admin(), 1, input); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("duplicate update code=%v", status.Code(err))
	}
}

type clockedContestRepo struct {
	*fakeContestRepo
	databaseTime time.Time
}

func (r *clockedContestRepo) CurrentTime(context.Context) (time.Time, error) {
	return r.databaseTime, nil
}

func TestContestLifecycleUsesRepositoryClockDespiteHostSkew(t *testing.T) {
	at := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	repo := &clockedContestRepo{fakeContestRepo: &fakeContestRepo{contest: Contest{ID: 1, StartAt: at, EndAt: at.Add(time.Hour), Status: contestv1.ContestStatus_CONTEST_STATUS_DRAFT}}, databaseTime: at}
	uc := NewContestUsecaseWithRepository(repo)
	uc.now = func() time.Time { return at.Add(24 * time.Hour) }
	c, err := uc.Get(t.Context(), admin(), 1)
	if err != nil || c.Status != contestv1.ContestStatus_CONTEST_STATUS_RUNNING {
		t.Fatalf("database start boundary status=%v err=%v", c.Status, err)
	}
	repo.databaseTime = at.Add(time.Hour)
	c, err = uc.Get(t.Context(), admin(), 1)
	if err != nil || c.Status != contestv1.ContestStatus_CONTEST_STATUS_ENDED {
		t.Fatalf("database end boundary status=%v err=%v", c.Status, err)
	}
}

func TestContestIntervalRejectsPrecisionCollapse(t *testing.T) {
	at := time.Date(2026, 10, 5, 1, 0, 0, 123000000, time.UTC)
	input := contestInput(at)
	input.EndAt = timestamppb.New(at.Add(time.Microsecond))
	if _, _, _, err := normalizeUpdate(input); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("collapsed interval error=%v", err)
	}
}

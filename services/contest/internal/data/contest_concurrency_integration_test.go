package data

import (
	"context"
	"sync"
	"testing"
	"time"

	contestv1 "github.com/viggggil/go_oj_agent/api/contest/v1"
	"github.com/viggggil/go_oj_agent/services/contest/internal/biz"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func integrationContest(t *testing.T, repo *Repository, start time.Time) biz.Contest {
	t.Helper()
	contest, err := repo.Create(context.Background(), biz.Contest{
		Title: "concurrency", StartAt: start, EndAt: start.Add(time.Hour), CreatedBy: 9,
		Status:   contestv1.ContestStatus_CONTEST_STATUS_DRAFT,
		Problems: []biz.ContestProblem{{ProblemID: 7, SortOrder: 1, Score: 100}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return contest
}

func TestContestConcurrentUpdatesUseOptimisticToken(t *testing.T) {
	db := testContestDatabase(t)
	repo := NewRepository(db)
	contest := integrationContest(t, repo, time.Now().UTC().Add(time.Hour))

	left := contest
	left.Title = "left"
	right := contest
	right.Title = "right"
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, candidate := range []biz.Contest{left, right} {
		candidate := candidate
		wg.Go(func() {
			_, err := repo.Update(context.Background(), candidate)
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	var successes, conflicts int
	for err := range errs {
		if err == nil {
			successes++
		} else if status.Code(err) == codes.Aborted {
			conflicts++
		} else {
			t.Fatalf("unexpected update error: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
}

func TestContestJoinAndArchiveAreSerialized(t *testing.T) {
	db := testContestDatabase(t)
	repo := NewRepository(db)
	contest := integrationContest(t, repo, time.Now().UTC().Add(time.Hour))
	var wg sync.WaitGroup
	joinErr := make(chan error, 1)
	archiveErr := make(chan error, 1)
	wg.Go(func() {
		_, err := repo.Join(context.Background(), contest.ID, 42)
		joinErr <- err
	})
	wg.Go(func() {
		_, err := repo.Archive(context.Background(), contest.ID)
		archiveErr <- err
	})
	wg.Wait()
	if err := <-archiveErr; err != nil {
		t.Fatalf("archive error: %v", err)
	}
	if err := <-joinErr; err != nil && status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("join error: %v", err)
	}
	final, err := repo.Get(context.Background(), contest.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != contestv1.ContestStatus_CONTEST_STATUS_ARCHIVED {
		t.Fatalf("status=%v", final.Status)
	}
	var joined int
	if err := db.QueryRow(`SELECT COUNT(*) FROM contest_participants WHERE contest_id=?`, contest.ID).Scan(&joined); err != nil {
		t.Fatal(err)
	}
	if joined > 1 {
		t.Fatalf("unexpected participant count=%d", joined)
	}
}

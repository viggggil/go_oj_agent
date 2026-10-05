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

// 归档先拿到锁时，等待中的报名必须重新读取提交后的状态。
func TestContestArchiveWinsBeforeWaitingJoin(t *testing.T) {
	db := testContestDatabase(t)
	repo := NewRepository(db)
	c := integrationContest(t, repo, time.Now().UTC().Add(time.Hour))
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE contests SET status='archived' WHERE id=?`, c.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := repo.Join(t.Context(), c.ID, 42); done <- err }()
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("join error = %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM contest_participants WHERE contest_id=?`, c.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("archived contest gained %d participants", n)
	}
}

func TestContestUnchangedUpdateAdvancesToken(t *testing.T) {
	db := testContestDatabase(t)
	repo := NewRepository(db)
	c := integrationContest(t, repo, time.Now().UTC().Add(time.Hour))
	updated, err := repo.Update(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.UpdatedAt.After(c.UpdatedAt) {
		t.Fatal("version token did not advance")
	}
	if _, err := repo.Update(t.Context(), c); status.Code(err) != codes.Aborted {
		t.Fatalf("stale update error = %v", err)
	}
}

func TestSubmissionAuthorizationWaitsForConcurrentConfigurationCommit(t *testing.T) {
	db := testContestDatabase(t)
	repo := NewRepository(db)
	c := integrationContest(t, repo, time.Now().UTC().Add(time.Hour))
	if _, err := repo.Join(t.Context(), c.ID, 42); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE contests SET start_at=UTC_TIMESTAMP(3)-INTERVAL 1 SECOND,end_at=UTC_TIMESTAMP(3)+INTERVAL 1 HOUR WHERE id=?`, c.ID); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	// 模拟跨开始边界尚未提交的配置事务；提交必须等待并使用新配置。
	if _, err := tx.Exec(`UPDATE contests SET start_at=UTC_TIMESTAMP(3)+INTERVAL 1 HOUR,end_at=UTC_TIMESTAMP(3)+INTERVAL 2 HOUR WHERE id=?`, c.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	go func() { _, err := repo.AuthorizeSubmission(ctx, c.ID, 42, 7); done <- err }()
	var schema string
	if err := db.QueryRow(`SELECT DATABASE()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case err := <-done:
			t.Fatalf("authorization bypassed configuration lock: %v", err)
		default:
		}
		var waiting int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM performance_schema.data_lock_waits w JOIN performance_schema.data_locks l ON l.ENGINE_LOCK_ID=w.REQUESTING_ENGINE_LOCK_ID AND l.ENGINE=w.ENGINE WHERE l.OBJECT_SCHEMA=? AND l.OBJECT_NAME='contests'`, schema).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting > 0 {
			break
		}
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
		time.Sleep(time.Millisecond)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("authorization used stale start_at: %v", err)
	}
}

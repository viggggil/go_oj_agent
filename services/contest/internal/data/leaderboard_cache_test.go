package data

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/viggggil/go_oj_agent/services/contest/internal/biz"
)

func expectSQLPage(mock sqlmock.Sqlmock, delay time.Duration) {
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT COUNT").WithArgs(int64(20)).WillDelayFor(delay).WillReturnRows(sqlmock.NewRows([]string{"total"}).AddRow(1))
	mock.ExpectQuery("SELECT user_id,SUM").WithArgs(int64(20), int32(100), int64(0)).WillReturnRows(sqlmock.NewRows([]string{"user", "solved", "penalty"}).AddRow(42, 0, 0))
	mock.ExpectQuery("SELECT p.problem_id").WithArgs(int64(42), int64(20)).WillReturnRows(sqlmock.NewRows([]string{"problem", "solved", "wrong", "accepted"}).AddRow(7, false, 2, nil))
	mock.ExpectCommit()
}
func TestCachedLeaderboardCoalescesFallback(t *testing.T) {
	server := miniredis.RunT(t)
	cache := testRedisCache(t, server.Addr())
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewCachedRepository(db, cache)
	contest := biz.Contest{ID: 20}
	expectSQLPage(mock, 100*time.Millisecond)
	expectSQLPage(mock, 0)
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for range 32 {
		wg.Go(func() {
			items, total, err := repo.CachedLeaderboard(t.Context(), contest, 1, 100)
			if err == nil && (total != 1 || len(items) != 1 || items[0].Problems[0].WrongAttempts != 2) {
				err = fmt.Errorf("fallback mismatch: %v %d", items, total)
			}
			// Each caller owns its messages; mutations must not leak into another page.
			if err == nil {
				items[0].Problems[0].WrongAttempts = 99
			}
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	items, _, err := repo.CachedLeaderboard(t.Context(), contest, 1, 100)
	if err != nil || items[0].Problems[0].WrongAttempts != 2 {
		t.Fatal("caller changed shared cached response", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal("miss storm did not coalesce", err)
	}
}
func TestCachedLeaderboardCancellationDoesNotPoisonOtherWaiters(t *testing.T) {
	server := miniredis.RunT(t)
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewCachedRepository(db, testRedisCache(t, server.Addr()))
	expectSQLPage(mock, 100*time.Millisecond)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, _, err := repo.CachedLeaderboard(ctx, biz.Contest{ID: 20}, 1, 100); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if items, total, err := repo.CachedLeaderboard(t.Context(), biz.Contest{ID: 20}, 1, 100); err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("cancelled first waiter poisoned shared call: %v %d %v", items, total, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestCachedLeaderboardHitDoesNotReadSQL(t *testing.T) {
	server := miniredis.RunT(t)
	cache := testRedisCache(t, server.Addr())
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewCachedRepository(db, cache)
	s := testSnapshot(42, "1", 1, 10)
	contest := snapshotContest(s)
	publishTestBuild(t, cache, contest, s)
	if items, total, err := repo.CachedLeaderboard(t.Context(), contest, 1, 100); err != nil || total != 1 || items[0].PenaltySeconds != 10 {
		t.Fatalf("cache hit: %v %d %v", items, total, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestCachedLeaderboardDisabled(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewCachedRepository(db, &LeaderboardRedis{})
	expectSQLPage(mock, 0)
	if _, total, err := repo.CachedLeaderboard(t.Context(), biz.Contest{ID: 20}, 1, 100); err != nil || total != 1 {
		t.Fatal(total, err)
	}
	repo.MaintainLeaderboards(t.Context())
	if repo.CacheEnabled() {
		t.Fatal("disabled cache started maintenance")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestCachedLeaderboardOutageCooldown(t *testing.T) {
	server := miniredis.RunT(t)
	cache := testRedisCache(t, server.Addr())
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewCachedRepository(db, cache)
	// Server failures cause a bounded circuit cooldown.
	server.SetError("ERR Redis unavailable")
	expectSQLPage(mock, 0)
	if _, _, err := repo.CachedLeaderboard(t.Context(), biz.Contest{ID: 20}, 1, 100); err != nil {
		t.Fatal(err)
	}
	if !time.Now().Before(repo.leaderboard.cooldown) {
		t.Fatal("Redis error did not open cooldown")
	}
	// Removing the error cannot cause repeated Redis probes during the cooldown.
	server.SetError("")
	if _, _, err := repo.CachedLeaderboard(t.Context(), biz.Contest{ID: 20}, 1, 100); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

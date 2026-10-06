package data

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/viggggil/go_oj_agent/pkg/mq"
	"github.com/viggggil/go_oj_agent/services/contest/internal/biz"
	"google.golang.org/protobuf/proto"
)

func seedFailureContest(t *testing.T, repo *Repository) (biz.Contest, biz.ProjectionEvent) {
	t.Helper()
	start := time.Now().UTC().Truncate(time.Millisecond).Add(-time.Hour)
	res, err := repo.db.Exec(`INSERT INTO contests(title,status,start_at,end_at,created_by,created_at,updated_at) VALUES ('failures','draft',?,?,1,?,?)`, start, start.Add(2*time.Hour), start, start)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	if _, err := repo.db.Exec(`INSERT INTO contest_problems VALUES (?,7,1,100)`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.Exec(`INSERT INTO contest_participants VALUES (?,42,?)`, id, start); err != nil {
		t.Fatal(err)
	}
	contest, err := repo.Get(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return contest, biz.ProjectionEvent{EventID: uuid.NewString(), Fact: biz.SubmissionFact{SubmissionJudged: mq.SubmissionJudged{SubmissionID: 1, ContestID: id, UserID: 42, ProblemID: 7, Verdict: "AC", SubmittedAt: start.Add(time.Minute), JudgedAt: start.Add(2 * time.Minute)}}}
}

type failedCacheConfirmation struct{ *Repository }

func (r failedCacheConfirmation) CompleteCacheEvent(context.Context, int64, string) error {
	return errors.New("process exited after Redis write")
}

func TestRedisWriteSurvivesFailedConfirmationAndRelayRestart(t *testing.T) {
	address := os.Getenv("CONTEST_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("set CONTEST_TEST_REDIS_ADDR and CONTEST_TEST_MYSQL_DSN")
	}
	db := testContestDatabase(t)
	cache := testRedisCache(t, address)
	repo := NewCachedRepository(db, cache)
	ctx := t.Context()
	contest, event := seedFailureContest(t, repo)
	if err := repo.RebuildLeaderboard(ctx, contest.ID); err != nil {
		t.Fatal(err)
	}
	// 模拟投影提交后原进程退出：新 relay 只使用持久化事件。
	if err := repo.ApplyProjection(ctx, event); err != nil {
		t.Fatal(err)
	}
	relay := &biz.CacheRelay{Repository: failedCacheConfirmation{repo}, Sink: cache}
	if claimed, err := relay.RunOnce(ctx); !claimed || err == nil {
		t.Fatalf("claimed=%v err=%v", claimed, err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM contest_cache_outbox WHERE status='pending' AND lease_owner IS NOT NULL`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("pending lease=%d err=%v", n, err)
	}
	if _, err := db.Exec(`UPDATE contest_cache_outbox SET lease_until=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE contest_id=?`, contest.ID); err != nil {
		t.Fatal(err)
	}
	newRepo := NewCachedRepository(db, cache)
	relay = &biz.CacheRelay{Repository: newRepo, Sink: cache}
	if claimed, err := relay.RunOnce(ctx); !claimed || err != nil {
		t.Fatalf("restart claimed=%v err=%v", claimed, err)
	}
	actual, total, err := cache.Read(ctx, contest, 1, 100)
	if err != nil || total != 1 || len(actual) != 1 || actual[0].SolvedCount != 1 || actual[0].PenaltySeconds != 60 {
		t.Fatalf("board=%v total=%d err=%v", actual, total, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM contest_cache_outbox WHERE status='applied'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("applied=%d err=%v", n, err)
	}
	if err := newRepo.ApplyProjection(ctx, event); err != nil {
		t.Fatal(err)
	}
	var version int64
	if err := db.QueryRow(`SELECT version FROM contest_user_results WHERE contest_id=? AND user_id=42`, contest.ID).Scan(&version); err != nil || version != 1 {
		t.Fatalf("duplicate advanced version=%d err=%v", version, err)
	}
}

func TestIndependentRepositoriesFenceExpiredBuilder(t *testing.T) {
	address := os.Getenv("CONTEST_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("set CONTEST_TEST_REDIS_ADDR and CONTEST_TEST_MYSQL_DSN")
	}
	db := testContestDatabase(t)
	cache := testRedisCache(t, address)
	first := NewCachedRepository(db, cache)
	second := NewCachedRepository(db, cache)
	ctx := t.Context()
	contest, event := seedFailureContest(t, first)
	if err := first.ApplyProjection(ctx, event); err != nil {
		t.Fatal(err)
	}
	old, err := cache.BeginBuild(ctx, contest.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.RebuildLeaderboard(ctx, contest.ID); !errors.Is(err, ErrBuildBusy) {
		t.Fatalf("second builder=%v", err)
	}
	// 删除本比赛的租约，模拟实例一暂停直至 lease 过期。
	if err := cache.client.Del(ctx, cache.base(contest.ID)+"lease").Err(); err != nil {
		t.Fatal(err)
	}
	if err := second.RebuildLeaderboard(ctx, contest.ID); err != nil {
		t.Fatal(err)
	}
	active, _, err := cache.pointers(ctx, contest.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.ActivateBuild(ctx, contest.ID, old, contestSignature(contest)); !errors.Is(err, ErrGenerationChanged) {
		t.Fatalf("expired activation=%v", err)
	}
	_ = cache.AbandonBuild(ctx, contest.ID, old)
	got, _, err := cache.Read(ctx, contest, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	want, _, err := second.Leaderboard(ctx, contest.ID, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !proto.Equal(got[0], want[0]) {
		t.Fatalf("active=%s board=%v SQL=%v", active, got, want)
	}
}

func TestSocketDisconnectAndRestartRebuildFromMySQL(t *testing.T) {
	db := testContestDatabase(t)
	redisServer := miniredis.RunT(t)
	cache := testRedisCache(t, redisServer.Addr())
	repo := NewCachedRepository(db, cache)
	ctx := t.Context()
	contest, event := seedFailureContest(t, repo)
	if err := repo.ApplyProjection(ctx, event); err != nil {
		t.Fatal(err)
	}
	if err := repo.RebuildLeaderboard(ctx, contest.ID); err != nil {
		t.Fatal(err)
	}
	relay := &biz.CacheRelay{Repository: repo, Sink: cache}
	if _, err := relay.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	redisServer.Close()
	// Redis socket 真断开；成绩仍能落库，榜单直接回源。
	event.EventID = uuid.NewString()
	event.Fact.Verdict = "WA"
	event.Fact.JudgedAt = event.Fact.JudgedAt.Add(time.Second)
	if err := repo.ApplyProjection(ctx, event); err != nil {
		t.Fatal(err)
	}
	got, _, err := repo.CachedLeaderboard(ctx, contest, 1, 100)
	if err != nil || len(got) != 1 || got[0].SolvedCount != 0 {
		t.Fatalf("outage board=%v err=%v", got, err)
	}
	if err := redisServer.Restart(); err != nil {
		t.Fatal(err)
	}
	redisServer.FlushAll()
	repo.leaderboard.mu.Lock()
	repo.leaderboard.cooldown = time.Time{}
	repo.leaderboard.mu.Unlock()
	if _, _, err := repo.CachedLeaderboard(ctx, contest, 1, 100); err != nil {
		t.Fatal(err)
	}
	repo.MaintainLeaderboards(ctx)
	if _, err := relay.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	got, _, err = cache.Read(ctx, contest, 1, 100)
	if err != nil || len(got) != 1 || got[0].SolvedCount != 0 {
		t.Fatalf("recovery board=%v err=%v", got, err)
	}
}

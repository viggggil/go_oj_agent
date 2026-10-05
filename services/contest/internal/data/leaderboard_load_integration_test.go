package data

import (
	"fmt"
	"os"
	"runtime"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/viggggil/go_oj_agent/pkg/mq"
	"github.com/viggggil/go_oj_agent/services/contest/internal/biz"
	"google.golang.org/protobuf/proto"
)

// 可重复的小规模并发负载；日志输出 QPS、P95/P99、命中率及数据库回源。
// 不把本机结果当成生产容量目标，CI 只要求正确性和最终收敛。
func TestLeaderboardLoadAndRecoveryProfile(t *testing.T) {
	address := os.Getenv("CONTEST_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("set CONTEST_TEST_REDIS_ADDR and CONTEST_TEST_MYSQL_DSN")
	}
	db := testContestDatabase(t)
	cache := testRedisCache(t, address)
	repo := NewCachedRepository(db, cache)
	ctx := t.Context()
	start := time.Now().UTC().Truncate(time.Millisecond).Add(-time.Hour)
	result, err := db.Exec(`INSERT INTO contests(title,status,start_at,end_at,created_by,created_at,updated_at) VALUES ('load','draft',?,?,9,?,?)`, start, start.Add(2*time.Hour), start, start)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := result.LastInsertId()
	for p := int64(1); p <= 10; p++ {
		if _, err := db.Exec(`INSERT INTO contest_problems VALUES (?,?,?,100)`, id, p, p); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for user := int64(1); user <= 100; user++ {
		if _, err := tx.Exec(`INSERT INTO contest_participants VALUES (?,?,?)`, id, user, start); err != nil {
			t.Fatal(err)
		}
		for p := int64(1); p <= 10; p++ {
			solved := p <= user%5
			var accepted any
			penalty := int64(0)
			if solved {
				accepted = start.Add(600 * time.Second)
				penalty = 600
				if _, err := tx.Exec(`INSERT INTO contest_submission_results VALUES (?,?,?,?,'AC',?,?,FALSE,UTC_TIMESTAMP(6))`, user*100+p, id, user, p, accepted, start.Add(601*time.Second)); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := tx.Exec(`INSERT INTO contest_problem_results (contest_id,user_id,problem_id,solved,wrong_attempts,accepted_at,penalty_seconds,updated_at) VALUES (?,?,?,?,0,?,?,UTC_TIMESTAMP(6))`, id, user, p, solved, accepted, penalty); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := tx.Exec(`INSERT INTO contest_user_results SELECT contest_id,user_id,SUM(solved),SUM(penalty_seconds),1,UTC_TIMESTAMP(6) FROM contest_problem_results WHERE contest_id=? GROUP BY contest_id,user_id`, id); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	contest, err := repo.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	rebuildStarted := time.Now()
	if err := repo.RebuildLeaderboard(ctx, id); err != nil {
		t.Fatal(err)
	}
	rebuildDuration := time.Since(rebuildStarted)
	sqlTimes := make([]time.Duration, 0, 100)
	sqlStarted := time.Now()
	for range 100 {
		at := time.Now()
		if _, _, err := repo.Leaderboard(ctx, id, 1, 20); err != nil {
			t.Fatal(err)
		}
		sqlTimes = append(sqlTimes, time.Since(at))
	}
	sqlDuration := time.Since(sqlStarted)
	before := CacheMetricsSnapshot()
	var wg sync.WaitGroup
	var mu sync.Mutex
	cacheTimes := make([]time.Duration, 0, 400)
	errs := make(chan error, 12)
	cacheStarted := time.Now()
	for range 8 {
		wg.Go(func() {
			for range 50 {
				at := time.Now()
				_, total, err := repo.CachedLeaderboard(ctx, contest, 1, 20)
				if err != nil || total != 100 {
					errs <- fmt.Errorf("cache read total=%d: %w", total, err)
					return
				}
				mu.Lock()
				cacheTimes = append(cacheTimes, time.Since(at))
				mu.Unlock()
			}
		})
	}
	for worker := int64(0); worker < 4; worker++ {
		wg.Go(func() {
			for j := int64(1); j <= 5; j++ {
				user := worker*5 + j
				f := mq.SubmissionJudged{SubmissionID: 100000 + user, ContestID: id, UserID: user, ProblemID: 10, Verdict: "AC", SubmittedAt: start.Add(20 * time.Minute), JudgedAt: time.Now().UTC()}
				if err := repo.ApplyProjection(ctx, biz.ProjectionEvent{EventID: uuid.NewString(), Fact: biz.SubmissionFact{SubmissionJudged: f}}); err != nil {
					errs <- err
					return
				}
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	cacheDuration := time.Since(cacheStarted)
	relayStarted := time.Now()
	relay := &biz.CacheRelay{Repository: repo, Sink: cache, Observe: RecordCacheRelayOutcome}
	for range 25 {
		claimed, err := relay.RunOnce(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !claimed {
			break
		}
	}
	relayDuration := time.Since(relayStarted)
	assertSame := func() {
		t.Helper()
		for page := int32(1); page <= 5; page++ {
			sqlRows, sqlTotal, err := repo.Leaderboard(ctx, id, page, 20)
			if err != nil {
				t.Fatal(err)
			}
			cached, cachedTotal, err := cache.Read(ctx, contest, page, 20)
			if err != nil || sqlTotal != cachedTotal || len(sqlRows) != len(cached) {
				t.Fatalf("page=%d total=%d/%d err=%v", page, sqlTotal, cachedTotal, err)
			}
			for i := range sqlRows {
				if !proto.Equal(sqlRows[i], cached[i]) {
					t.Fatalf("page=%d row=%d mismatch", page, i)
				}
			}
		}
	}
	assertSame()
	after := CacheMetricsSnapshot()
	// 清掉本测试 namespace 的榜单，完全从 SQL 重建，不依赖 applied Outbox。
	keys, err := cache.client.Keys(ctx, cache.namespace+":*").Result()
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.client.Del(ctx, keys...).Err(); err != nil {
		t.Fatal(err)
	}
	recoverStarted := time.Now()
	if err := repo.RebuildLeaderboard(ctx, id); err != nil {
		t.Fatal(err)
	}
	recovery := time.Since(recoverStarted)
	assertSame()
	percentile := func(samples []time.Duration, p int) time.Duration {
		sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
		return samples[(len(samples)-1)*p/100]
	}
	hits := after["cache_hits"] - before["cache_hits"]
	misses := after["cache_misses"] - before["cache_misses"]
	t.Logf("env=%s/%s cpu=%d participants=100 problems=10 page=20 writers=4 events=20 readers=8 requests=400 cache_QPS=%.1f hit_rate=%.3f SQL_fallbacks=%d cache_P95=%s cache_P99=%s SQL_QPS=%.1f SQL_P95=%s SQL_P99=%s result_writes_per_sec=%.1f relay_20_events=%s rebuild=%s loss_recovery=%s", runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), 400/cacheDuration.Seconds(), float64(hits)/float64(hits+misses), after["sql_fallbacks"]-before["sql_fallbacks"], percentile(cacheTimes, 95), percentile(cacheTimes, 99), 100/sqlDuration.Seconds(), percentile(sqlTimes, 95), percentile(sqlTimes, 99), 20/cacheDuration.Seconds(), relayDuration, rebuildDuration, recovery)
}

package data

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/viggggil/go_oj_agent/services/contest/internal/biz"
)

func snapshotContest(s biz.LeaderboardSnapshot) biz.Contest {
	c := biz.Contest{ID: s.ContestID, StartAt: time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC), EndAt: time.Date(2026, 10, 4, 3, 0, 0, 0, time.UTC)}
	for i, p := range s.Problems {
		c.Problems = append(c.Problems, biz.ContestProblem{ProblemID: p.ProblemID, SortOrder: int32(i + 1), Score: 100})
	}
	return c
}
func publishTestBuild(t *testing.T, cache *LeaderboardRedis, c biz.Contest, snapshots ...biz.LeaderboardSnapshot) string {
	t.Helper()
	g, err := cache.BeginBuild(t.Context(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range snapshots {
		if err := cache.Load(t.Context(), g, s); err != nil {
			t.Fatal(err)
		}
	}
	if err := cache.ActivateBuild(t.Context(), c.ID, g, contestSignature(c)); err != nil {
		t.Fatal(err)
	}
	if err := cache.VerifyFreshness(t.Context(), c.ID, g, contestSignature(c), 0); err != nil {
		t.Fatal(err)
	}
	return g
}
func exerciseGenerations(t *testing.T, cache *LeaderboardRedis) {
	ctx := t.Context()
	base := testSnapshot(42, "1", 2, 600)
	contest := snapshotContest(base)
	t.Run("empty ready and pagination", func(t *testing.T) {
		publishTestBuild(t, cache, contest)
		items, total, err := cache.Read(ctx, contest, 1, 100)
		if err != nil || total != 0 || len(items) != 0 {
			t.Fatalf("empty ready: %v %d %v", items, total, err)
		}
		for _, user := range []int64{10, 2, 42} {
			if err := cache.Apply(ctx, testSnapshot(user, "1", 1, 99)); err != nil {
				t.Fatal(err)
			}
		}
		items, total, err = cache.Read(ctx, contest, 1, 2)
		if err != nil || total != 3 || len(items) != 2 || items[0].UserId != 2 || items[1].UserId != 10 || items[1].Rank != 2 || len(items[0].Problems) != 100 {
			t.Fatalf("first page: %v %d %v", items, total, err)
		}
		items, total, err = cache.Read(ctx, contest, 2, 2)
		if err != nil || total != 3 || len(items) != 1 || items[0].UserId != 42 || items[0].Rank != 3 {
			t.Fatalf("last page: %v %d %v", items, total, err)
		}
		items, total, err = cache.Read(ctx, contest, 3, 2)
		if err != nil || total != 3 || len(items) != 0 {
			t.Fatalf("after last page: %v %d %v", items, total, err)
		}
		if _, _, err := cache.Read(ctx, contest, 1, 101); err == nil {
			t.Fatal("unbounded page accepted")
		}
	})
	t.Run("snapshot and dual write catch up", func(t *testing.T) {
		active := publishTestBuild(t, cache, contest, base)
		build, err := cache.BeginBuild(ctx, contest.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := cache.BeginBuild(ctx, contest.ID); !errors.Is(err, ErrBuildBusy) {
			t.Fatal("concurrent builder was not fenced", err)
		}
		latest := testSnapshot(42, "9007199254740993", 0, 0)
		if err := cache.Apply(ctx, latest); err != nil {
			t.Fatal(err)
		}
		// This simulates a row from a read view established BEFORE the latest result.
		if err := cache.Load(ctx, build, base); err != nil {
			t.Fatal(err)
		}
		newcomer := testSnapshot(99, "1", 1, 20)
		if err := cache.Apply(ctx, newcomer); err != nil {
			t.Fatal(err)
		}
		for _, g := range []string{active, build} {
			got := cachedSnapshot(t, cache, cache.generationKeys(20, g)[1], 42)
			if got.Version != latest.Version || got.SolvedCount != 0 {
				t.Fatalf("lost rejudge in %s: %+v", g, got)
			}
		}
		if err := cache.ActivateBuild(ctx, 20, build, contestSignature(contest)); err != nil {
			t.Fatal(err)
		}
		// New ready pointer alone cannot claim freshness before SQL lag verification.
		if _, _, err := cache.Read(ctx, contest, 1, 100); !errors.Is(err, ErrCacheStale) {
			t.Fatal(err)
		}
		if err := cache.VerifyFreshness(ctx, 20, build, contestSignature(contest), 0); err != nil {
			t.Fatal(err)
		}
		items, total, err := cache.Read(ctx, contest, 1, 100)
		if err != nil || total != 2 || items[0].UserId != 99 || items[1].UserId != 42 {
			t.Fatalf("cutover: %v %d %v", items, total, err)
		}
		if err := cache.Apply(ctx, base); err != nil {
			t.Fatal(err)
		}
		if got := cachedSnapshot(t, cache, cache.generationKeys(20, build)[1], 42); got.Version != latest.Version {
			t.Fatal("old event revived old scores")
		}
	})
	t.Run("expired owner cannot load renew switch or clean new owner", func(t *testing.T) {
		active := publishTestBuild(t, cache, contest, base)
		old, err := cache.BeginBuild(ctx, 20)
		if err != nil {
			t.Fatal(err)
		}
		// Simulate both TTLs expiring without waiting 30 seconds.
		cache.client.Del(ctx, cache.base(20)+"lease", cache.base(20)+"building")
		current, err := cache.BeginBuild(ctx, 20)
		if err != nil {
			t.Fatal(err)
		}
		if err := cache.RenewBuild(ctx, 20, old); !errors.Is(err, ErrGenerationChanged) {
			t.Fatal(err)
		}
		if err := cache.Load(ctx, old, base); !errors.Is(err, ErrGenerationChanged) {
			t.Fatal(err)
		}
		if err := cache.ActivateBuild(ctx, 20, old, contestSignature(contest)); !errors.Is(err, ErrGenerationChanged) {
			t.Fatal(err)
		}
		if err := cache.AbandonBuild(ctx, 20, old); err != nil {
			t.Fatal(err)
		}
		if pointer := cache.client.Get(ctx, cache.base(20)+"building").Val(); pointer != current {
			t.Fatal("old owner cleared new build", pointer)
		}
		if pointer := cache.client.Get(ctx, cache.base(20)+"active").Val(); pointer != active {
			t.Fatal("old owner switched active", pointer)
		}
		if err := cache.AbandonBuild(ctx, 20, current); err != nil {
			t.Fatal(err)
		}
		if _, total, err := cache.Read(ctx, contest, 1, 100); err != nil || total != 1 {
			t.Fatalf("failed build damaged old readable board: %d %v", total, err)
		}
	})
	t.Run("relay selected pointers before registration must retry", func(t *testing.T) {
		old := publishTestBuild(t, cache, contest, base)
		args, _ := snapshotArguments(testSnapshot(42, "2", 0, 0), old)
		args = append(args, "")
		keys := append([]string{cache.base(20) + "active", cache.base(20) + "building", cache.base(20) + "lease"}, cache.generationKeys(20, old)...)
		keys = append(keys, cache.generationKeys(20, "")...)
		build, err := cache.BeginBuild(ctx, 20)
		if err != nil {
			t.Fatal(err)
		}
		result, err := relayLeaderboard.Run(ctx, cache.client, keys, args...).Int()
		if err != nil || result != -3 {
			t.Fatalf("old-only ACK allowed after registration: %d %v", result, err)
		}
		cache.AbandonBuild(ctx, 20, build)
	})
	t.Run("dirty active still catches up building without ACK", func(t *testing.T) {
		active := publishTestBuild(t, cache, contest, base)
		build, err := cache.BeginBuild(ctx, 20)
		if err != nil {
			t.Fatal(err)
		}
		cache.client.HSet(ctx, cache.generationKeys(20, active)[4], "state", "dirty")
		newest := testSnapshot(42, "2", 0, 0)
		if err := cache.Apply(ctx, newest); !errors.Is(err, ErrCacheIncomplete) {
			t.Fatal(err)
		}
		if got := cachedSnapshot(t, cache, cache.generationKeys(20, build)[1], 42); got.Version != "2" {
			t.Fatal("building was not updated after broken active")
		}
		if err := cache.Load(ctx, build, base); err != nil {
			t.Fatal(err)
		}
		if err := cache.ActivateBuild(ctx, 20, build, contestSignature(contest)); err != nil {
			t.Fatal(err)
		}
		if err := cache.Apply(ctx, newest); err != nil {
			t.Fatal("pending event cannot retry after repair", err)
		}
	})
	t.Run("staleness structural faults and configuration", func(t *testing.T) {
		for _, fault := range []string{"old check", "queue lag", "missing entries", "missing user", "bad JSON", "bad JSON totals", "bad version", "dirty", "changed problems", "changed time"} {
			t.Run(fault, func(t *testing.T) {
				g := publishTestBuild(t, cache, contest, base)
				keys := cache.generationKeys(20, g)
				expected := contest
				switch fault {
				case "old check":
					cache.client.HSet(ctx, keys[4], "checked_ms", "1")
				case "queue lag":
					if err := cache.VerifyFreshness(ctx, 20, g, contestSignature(contest), 31*time.Second); err != nil {
						t.Fatal(err)
					}
				case "missing entries":
					cache.client.Del(ctx, keys[1])
				case "missing user":
					cache.client.HDel(ctx, keys[1], "42")
				case "bad JSON":
					cache.client.HSet(ctx, keys[1], "42", "{")
				case "bad JSON totals":
					broken := base
					broken.SolvedCount = 99
					raw, _ := json.Marshal(broken)
					cache.client.HSet(ctx, keys[1], "42", raw)
				case "bad version":
					cache.client.HSet(ctx, keys[2], "42", fmt.Sprintf("%019d", 2))
				case "dirty":
					cache.client.HSet(ctx, keys[4], "state", "dirty")
				case "changed problems":
					expected.Problems = append([]biz.ContestProblem(nil), contest.Problems...)
					expected.Problems[0].ProblemID = 1001
				case "changed time":
					expected.StartAt = expected.StartAt.Add(time.Hour)
				}
				if _, _, err := cache.Read(ctx, expected, 1, 100); err == nil {
					t.Fatal("fault returned silent/inconsistent cache page")
				}
			})
		}
	})
	t.Run("concurrent reads observe whole pages", func(t *testing.T) {
		publishTestBuild(t, cache, contest, base)
		var wg sync.WaitGroup
		errs := make(chan error, 40)
		for i := int64(1); i <= 20; i++ {
			wg.Go(func() { errs <- cache.Apply(ctx, testSnapshot(i, "1", 1, i)) })
			wg.Go(func() {
				items, total, err := cache.Read(ctx, contest, 1, 100)
				if err == nil && int64(len(items)) != total {
					err = fmt.Errorf("page=%d total=%d", len(items), total)
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
	})
}
func TestLeaderboardGenerations(t *testing.T) {
	server := miniredis.RunT(t)
	exerciseGenerations(t, testRedisCache(t, server.Addr()))
}
func TestRealLeaderboardGenerations(t *testing.T) {
	address := os.Getenv("CONTEST_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("set CONTEST_TEST_REDIS_ADDR")
	}
	exerciseGenerations(t, testRedisCache(t, address))
}
func TestGenerationTTL(t *testing.T) {
	server := miniredis.RunT(t)
	cache := testRedisCache(t, server.Addr())
	base := testSnapshot(42, "1", 1, 20)
	contest := snapshotContest(base)
	old := publishTestBuild(t, cache, contest, base)
	building, err := cache.BeginBuild(t.Context(), 20)
	if err != nil {
		t.Fatal(err)
	}
	server.FastForward(buildLease + time.Second)
	if err := cache.ActivateBuild(t.Context(), 20, building, contestSignature(contest)); !errors.Is(err, ErrGenerationChanged) {
		t.Fatal(err)
	}
	if err := cache.Load(t.Context(), building, base); !errors.Is(err, ErrGenerationChanged) {
		t.Fatal(err)
	}
	// Refresh active keys only; abandoned build/retired generations are bounded.
	next := publishTestBuild(t, cache, contest, base)
	server.FastForward(9 * time.Minute)
	if err := cache.VerifyFreshness(t.Context(), 20, next, contestSignature(contest), 0); err != nil {
		t.Fatal(err)
	}
	server.FastForward(2 * time.Minute)
	for _, g := range []string{old, building} {
		if count := cache.client.Exists(t.Context(), cache.generationKeys(20, g)...).Val(); count != 0 {
			t.Fatalf("retired generation leaked %d keys", count)
		}
	}
	if _, total, err := cache.Read(t.Context(), contest, 1, 100); err != nil || total != 1 {
		t.Fatalf("active expired independently: %d %v", total, err)
	}
}

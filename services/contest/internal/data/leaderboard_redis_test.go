package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/viggggil/go_oj_agent/services/contest/internal/biz"
	"github.com/viggggil/go_oj_agent/services/contest/internal/conf"
)

func testRedisCache(t *testing.T, address string) *LeaderboardRedis {
	t.Helper()
	cache, closeCache, err := NewLeaderboardRedis(&conf.Bootstrap{LeaderboardCache: &conf.LeaderboardCacheProto{Enabled: true, Addresses: []string{address}, Namespace: "test:" + uuid.NewString(), Timeout: "1s"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closeCache)
	t.Cleanup(func() {
		// Delete only this test's keys, never flush the shared Redis database.
		keys, err := cache.client.Keys(context.Background(), cache.namespace+":*").Result()
		if err == nil && len(keys) > 0 {
			_ = cache.client.Del(context.Background(), keys...).Err()
		}
	})
	return cache
}

func testActiveGeneration(t *testing.T, cache *LeaderboardRedis, contestID int64) []string {
	t.Helper()
	generation := uuid.NewString()
	if err := cache.PrepareGeneration(t.Context(), contestID, generation); err != nil {
		t.Fatal(err)
	}
	// Tests alone publish an empty build target. Production activation needs the
	// database snapshot/catch-up protocol from PR3 and is deliberately unavailable.
	if err := cache.client.Set(t.Context(), cache.base(contestID)+"active", generation, 0).Err(); err != nil {
		t.Fatal(err)
	}
	return cache.generationKeys(contestID, generation)
}

func testSnapshot(user int64, version string, solved int32, penalty int64) biz.LeaderboardSnapshot {
	if solved == 0 {
		penalty = 0
	}
	s := biz.LeaderboardSnapshot{Schema: 1, ContestID: 20, UserID: user, Version: version, SolvedCount: solved, PenaltySeconds: penalty}
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	for i := int32(0); i < 100; i++ {
		p := biz.LeaderboardProblem{ProblemID: int64(i) + 1}
		if i < solved {
			p.Solved = true
			p.AcceptedAt = &now
			if i == 0 {
				p.PenaltySeconds = penalty
			}
		}
		s.Problems = append(s.Problems, p)
	}
	return s
}

func cachedSnapshot(t *testing.T, cache *LeaderboardRedis, key string, user int64) biz.LeaderboardSnapshot {
	t.Helper()
	raw, err := cache.client.HGet(t.Context(), key, strconv.FormatInt(user, 10)).Bytes()
	if err != nil {
		t.Fatal(err)
	}
	var s biz.LeaderboardSnapshot
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func exerciseRedisLeaderboard(t *testing.T, cache *LeaderboardRedis) {
	t.Run("versions and rejudge", func(t *testing.T) {
		keys := testActiveGeneration(t, cache, 20)
		// Adjacent versions above 2^53 must stay distinct; a rejudge may lower scores.
		for _, s := range []biz.LeaderboardSnapshot{
			testSnapshot(42, "9007199254740992", 2, 600),
			testSnapshot(42, "9007199254740993", 0, 0),
			testSnapshot(42, "9007199254740992", 2, 600),
			testSnapshot(42, "9007199254740993", 0, 0),
		} {
			if err := cache.Apply(t.Context(), s); err != nil {
				t.Fatal(err)
			}
		}
		got := cachedSnapshot(t, cache, keys[1], 42)
		if got.Version != "9007199254740993" || got.SolvedCount != 0 {
			t.Fatalf("old event overwrote rejudge: %+v", got)
		}
		rank, err := cache.client.ZRange(t.Context(), keys[0], 0, -1).Result()
		if err != nil || len(rank) != 2 || rank[0] != leaderboardMember(got) || rank[1] != "~" {
			t.Fatalf("old rank member left behind: %v, %v", rank, err)
		}
		for _, v := range []string{"9223372036854775807", "9223372036854775806"} {
			if err := cache.Apply(t.Context(), testSnapshot(42, v, 1, math.MaxInt64)); err != nil {
				t.Fatal(err)
			}
		}
		if got := cachedSnapshot(t, cache, keys[1], 42); got.Version != "9223372036854775807" {
			t.Fatal(got.Version)
		}
	})
	t.Run("ordering and integer boundaries", func(t *testing.T) {
		keys := testActiveGeneration(t, cache, 20)
		snapshots := []biz.LeaderboardSnapshot{testSnapshot(10, "1", 1, 99), testSnapshot(2, "1", 1, 99), testSnapshot(math.MaxInt64, "1", 1, math.MaxInt64), testSnapshot(1, "1", 0, 0), testSnapshot(9, "1", 100, math.MaxInt64)}
		for _, s := range snapshots {
			if err := cache.Apply(t.Context(), s); err != nil {
				t.Fatal(err)
			}
		}
		got, err := cache.client.ZRange(t.Context(), keys[0], 0, -1).Result()
		want := []int{4, 1, 0, 2, 3}
		if err != nil || len(got) != len(want)+1 {
			t.Fatalf("rank=%v err=%v", got, err)
		}
		for i, j := range want {
			if got[i] != leaderboardMember(snapshots[j]) {
				t.Fatalf("rank=%v", got)
			}
		}
	})
	t.Run("concurrent complete snapshots", func(t *testing.T) {
		keys := testActiveGeneration(t, cache, 20)
		var wg sync.WaitGroup
		errs := make(chan error, 48)
		for user := int64(1); user <= 4; user++ {
			for v := int64(1); v <= 12; v++ {
				wg.Go(func() {
					errs <- cache.Apply(t.Context(), testSnapshot(user, strconv.FormatInt(v, 10), int32(v%3), v*10))
				})
			}
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		for user := int64(1); user <= 4; user++ {
			if got := cachedSnapshot(t, cache, keys[1], user); got.Version != "12" || got.SolvedCount != 0 {
				t.Fatal(got)
			}
		}
		if n := cache.client.ZCard(t.Context(), keys[0]).Val(); n != 5 {
			t.Fatalf("rank has %d members", n)
		}
	})
	t.Run("partial cache fails closed", func(t *testing.T) {
		for _, mutation := range []string{"missing rank", "missing entries", "missing versions", "missing members", "missing meta", "wrong type", "missing sentinel", "missing user version", "dirty", "wrong generation", "corrupt version", "corrupt member"} {
			t.Run(mutation, func(t *testing.T) {
				keys := testActiveGeneration(t, cache, 20)
				s := testSnapshot(42, "1", 1, 10)
				if err := cache.Apply(t.Context(), s); err != nil {
					t.Fatal(err)
				}
				ctx := t.Context()
				switch mutation {
				case "missing rank":
					cache.client.Del(ctx, keys[0])
				case "missing entries":
					cache.client.Del(ctx, keys[1])
				case "missing versions":
					cache.client.Del(ctx, keys[2])
				case "missing members":
					cache.client.Del(ctx, keys[3])
				case "missing meta":
					cache.client.Del(ctx, keys[4])
				case "wrong type":
					cache.client.Del(ctx, keys[1])
					cache.client.Set(ctx, keys[1], "invalid", 0)
				case "missing sentinel":
					cache.client.ZRem(ctx, keys[0], "~")
				case "missing user version":
					cache.client.HDel(ctx, keys[2], "42")
				case "dirty":
					cache.client.HSet(ctx, keys[4], "state", "dirty")
				case "wrong generation":
					cache.client.HSet(ctx, keys[3], "__generation", uuid.NewString())
				case "corrupt version":
					cache.client.HSet(ctx, keys[2], "42", "9999999999999999999")
				case "corrupt member":
					cache.client.HSet(ctx, keys[3], "42", leaderboardMember(testSnapshot(43, "1", 1, 10)))
				}
				// Compare serialized keys, proving rejection happens before any writes.
				before := make([]string, len(keys))
				for i, key := range keys {
					before[i] = testKeyContents(t, cache, key)
				}
				s.Version = "2"
				if err := cache.Apply(ctx, s); !errors.Is(err, ErrCacheIncomplete) {
					t.Fatalf("err=%v", err)
				}
				for i, key := range keys {
					if got := testKeyContents(t, cache, key); got != before[i] {
						t.Fatalf("rejected write modified %s", key)
					}
				}
			})
		}
	})
	t.Run("cold cache and invalid snapshot", func(t *testing.T) {
		cache.client.Del(t.Context(), cache.base(20)+"active")
		s := testSnapshot(42, "1", 1, 10)
		if err := cache.Apply(t.Context(), s); !errors.Is(err, ErrCacheIncomplete) {
			t.Fatal(err)
		}
		keys := testActiveGeneration(t, cache, 20)
		s.Version = "9223372036854775808"
		if err := cache.Apply(t.Context(), s); !errors.Is(err, biz.ErrInvalidSnapshot) {
			t.Fatal(err)
		}
		if cache.client.ZCard(t.Context(), keys[0]).Val() != 1 {
			t.Fatal("invalid snapshot wrote a rank member")
		}
	})
	t.Run("generation allocation and pointer fence", func(t *testing.T) {
		keys := testActiveGeneration(t, cache, 20)
		generation := cache.client.Get(t.Context(), cache.base(20)+"active").Val()
		if err := cache.PrepareGeneration(t.Context(), 20, generation); !errors.Is(err, ErrCacheIncomplete) {
			t.Fatal("existing generation overwritten", err)
		}
		next := uuid.NewString()
		partial := cache.generationKeys(20, next)
		cache.client.Set(t.Context(), partial[1], "partial", 0)
		if err := cache.PrepareGeneration(t.Context(), 20, next); !errors.Is(err, ErrCacheIncomplete) {
			t.Fatal(err)
		}
		if cache.client.Exists(t.Context(), partial[0], partial[4]).Val() != 0 {
			t.Fatal("partially existing generation modified")
		}
		// Switching after the Go GET but before EVAL must fence the old script.
		cache.client.Set(t.Context(), cache.base(20)+"active", uuid.NewString(), 0)
		s := testSnapshot(42, "1", 1, 10)
		payload, _ := json.Marshal(s)
<<<<<<< HEAD
		result, err := relayLeaderboard.Run(t.Context(), cache.client, append(append([]string{cache.base(20) + "active", cache.base(20) + "building", cache.base(20) + "lease"}, keys...), cache.generationKeys(20, "")...), generation, "42", fmt.Sprintf("%019d", 1), leaderboardMember(s), string(payload), fmt.Sprintf("%019d", 42), "").Int()
=======
		result, err := applyLeaderboard.Run(t.Context(), cache.client, append([]string{cache.base(20) + "active"}, keys...), generation, "42", fmt.Sprintf("%019d", 1), leaderboardMember(s), string(payload), fmt.Sprintf("%019d", 42)).Int()
>>>>>>> origin/main
		if err != nil || result != -3 || cache.client.ZCard(t.Context(), keys[0]).Val() != 1 {
			t.Fatalf("generation fence result=%d err=%v", result, err)
		}
	})
}

func TestLeaderboardRedis(t *testing.T) {
	r := miniredis.RunT(t)
	exerciseRedisLeaderboard(t, testRedisCache(t, r.Addr()))
}

func TestRealRedisLeaderboard(t *testing.T) {
	address := os.Getenv("CONTEST_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("set CONTEST_TEST_REDIS_ADDR for real Redis Lua tests")
	}
	exerciseRedisLeaderboard(t, testRedisCache(t, address))
	testRedisPartialWriteFailure(t, address)
}

func TestLeaderboardRedisOptional(t *testing.T) {
	cache, cleanup, err := NewLeaderboardRedis(&conf.Bootstrap{})
	if err != nil || cache.Enabled() {
		t.Fatalf("disabled cache: %v", err)
	}
	cleanup()
	// No Ping in the constructor, and an offline sink returns within its deadline.
	cache, cleanup, err = NewLeaderboardRedis(&conf.Bootstrap{LeaderboardCache: &conf.LeaderboardCacheProto{Enabled: true, Addresses: []string{"127.0.0.1:1"}, Namespace: "test", Timeout: "50ms"}})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	start := time.Now()
	if err := cache.Apply(t.Context(), testSnapshot(42, "1", 1, 10)); err == nil {
		t.Fatal("offline Redis write succeeded")
	}
	if time.Since(start) > time.Second {
		t.Fatal("Redis failure exceeded bounded timeout")
	}
}

func testKeyContents(t *testing.T, cache *LeaderboardRedis, key string) string {
	t.Helper()
	ctx := t.Context()
	kind, err := cache.client.Type(ctx, key).Result()
	if err != nil {
		t.Fatal(err)
	}
	var value any
	switch kind {
	case "none":
		return "missing"
	case "string":
		value, err = cache.client.Get(ctx, key).Result()
	case "hash":
		value, err = cache.client.HGetAll(ctx, key).Result()
	case "zset":
		value, err = cache.client.ZRangeWithScores(ctx, key, 0, -1).Result()
	default:
		t.Fatalf("unexpected test key type %s", kind)
	}
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return kind + ":" + string(raw)
}

// Exercise the real Outbox and Lua together inside the isolated MySQL fixture.
func testMySQLRedisRelay(t *testing.T, repo *Repository, contestID int64, address string) {
	t.Helper()
	ctx := t.Context()
	cache := testRedisCache(t, address)
	offline, closeOffline, err := NewLeaderboardRedis(&conf.Bootstrap{LeaderboardCache: &conf.LeaderboardCacheProto{Enabled: true, Addresses: []string{"127.0.0.1:1"}, Namespace: cache.namespace, Timeout: "50ms"}})
	if err != nil {
		t.Fatal(err)
	}
	defer closeOffline()
	relay := &biz.CacheRelay{Repository: repo, Sink: offline}
	if claimed, err := relay.RunOnce(ctx); err != nil || !claimed {
		t.Fatalf("offline attempt: %v %v", claimed, err)
	}
	var status string
	var retries int
	if err := repo.db.QueryRow(`SELECT status,retry_count FROM contest_cache_outbox WHERE contest_id=?`, contestID).Scan(&status, &retries); err != nil || status != "pending" || retries != 1 {
		t.Fatalf("offline delivery status=%s retries=%d err=%v", status, retries, err)
	}
	retryNow := func() {
		t.Helper()
		if _, err := repo.db.Exec(`UPDATE contest_cache_outbox SET next_retry_at=UTC_TIMESTAMP(6) WHERE contest_id=?`, contestID); err != nil {
			t.Fatal(err)
		}
	}
	retryNow()
	relay.Sink = cache
	// Cold cache is never silently initialized from a single user's snapshot.
	if claimed, err := relay.RunOnce(ctx); err != nil || !claimed {
		t.Fatalf("cold attempt: %v %v", claimed, err)
	}
	if err := repo.db.QueryRow(`SELECT status,retry_count FROM contest_cache_outbox WHERE contest_id=?`, contestID).Scan(&status, &retries); err != nil || status != "pending" || retries != 2 {
		t.Fatalf("cold delivery status=%s retries=%d err=%v", status, retries, err)
	}
	keys := testActiveGeneration(t, cache, contestID)
	retryNow()
	// A crash/error after Lua succeeds but before Outbox ACK must safely replay.
	once := &failFirstCompletion{Repository: repo}
	relay.Repository = once
	if claimed, err := relay.RunOnce(ctx); !claimed || err == nil {
		t.Fatalf("expected lost confirmation: %v %v", claimed, err)
	}
	if _, err := repo.db.Exec(`UPDATE contest_cache_outbox SET lease_until=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE contest_id=?`, contestID); err != nil {
		t.Fatal(err)
	}
	relay.Repository = repo
	if claimed, err := relay.RunOnce(ctx); err != nil || !claimed {
		t.Fatalf("replay: %v %v", claimed, err)
	}
	var payload []byte
	if err := repo.db.QueryRow(`SELECT status,payload FROM contest_cache_outbox WHERE contest_id=?`, contestID).Scan(&status, &payload); err != nil || status != "applied" {
		t.Fatalf("confirmation=%s err=%v", status, err)
	}
	got := cachedSnapshot(t, cache, keys[1], 42)
	encoded, err := json.Marshal(got)
	var want biz.LeaderboardSnapshot
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(payload, &want); err != nil {
		t.Fatal(err)
	}
	expected, _ := json.Marshal(want)
	if string(encoded) != string(expected) || cache.client.ZCard(ctx, keys[0]).Val() != 2 {
		t.Fatalf("cache differs from MySQL Outbox: got=%s want=%s", encoded, expected)
	}
	if claimed, err := relay.RunOnce(ctx); claimed || err != nil {
		t.Fatalf("completed event replayed: %v %v", claimed, err)
	}
}

type failFirstCompletion struct{ *Repository }

func (*failFirstCompletion) CompleteCacheEvent(context.Context, int64, string) error {
	return errors.New("simulated crash after Redis write")
}

func testRedisPartialWriteFailure(t *testing.T, address string) {
	t.Helper()
	admin := testRedisCache(t, address)
	keys := testActiveGeneration(t, admin, 20)
	s := testSnapshot(42, "1", 1, 10)
	if err := admin.Apply(t.Context(), s); err != nil {
		t.Fatal(err)
	}
	// An isolated ACL identity rejects ZADD after the dirty marker and old ZREM.
	// This provokes a real mid-script error without changing server memory limits
	// or affecting other clients/keys. Redis scripts do not roll back the ZREM.
	username := "test_" + uuid.NewString()
	password := uuid.NewString()
	if err := admin.client.Do(t.Context(), "ACL", "SETUSER", username, "on", ">"+password, "~"+admin.namespace+":*", "+@all", "-zadd").Err(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.client.Do(context.Background(), "ACL", "DELUSER", username).Err() })
	client := redis.NewClient(&redis.Options{Addr: address, Username: username, Password: password, MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	restricted := &LeaderboardRedis{client: client, enabled: true, namespace: admin.namespace, timeout: time.Second}
	s.Version = "2"
	if err := restricted.Apply(t.Context(), s); !errors.Is(err, ErrCacheIncomplete) {
		t.Fatalf("partial write err=%v", err)
	}
	if state := admin.client.HGet(t.Context(), keys[4], "state").Val(); state != "dirty" {
		t.Fatalf("partial write lost dirty fence: %s", state)
	}
	if admin.client.ZCard(t.Context(), keys[0]).Val() != 1 {
		t.Fatal("test did not produce an error after ZREM")
	}
	if err := admin.Apply(t.Context(), s); !errors.Is(err, ErrCacheIncomplete) {
		t.Fatalf("dirty generation accepted another write: %v", err)
	}
}

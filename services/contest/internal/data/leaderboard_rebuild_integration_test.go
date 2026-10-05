package data

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/viggggil/go_oj_agent/pkg/mq"
	"github.com/viggggil/go_oj_agent/services/contest/internal/biz"
	"google.golang.org/protobuf/proto"
)

func TestMySQLRedisLeaderboardRebuild(t *testing.T) {
	address := os.Getenv("CONTEST_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("set CONTEST_TEST_REDIS_ADDR and CONTEST_TEST_MYSQL_DSN")
	}
	db := testContestDatabase(t)
	cache := testRedisCache(t, address)
	repo := NewCachedRepository(db, cache)
	ctx := t.Context()
	start := time.Now().UTC().Truncate(time.Millisecond).Add(-time.Hour)
	res, err := db.Exec(`INSERT INTO contests(title,status,start_at,end_at,created_by,created_at,updated_at) VALUES ('rebuild','draft',?,?,1,?,?)`, start, start.Add(2*time.Hour), start, start)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	if _, err := db.Exec(`INSERT INTO contest_problems VALUES (?,7,1,100),(?,8,2,100)`, id, id); err != nil {
		t.Fatal(err)
	}
	for _, user := range []int64{2, 10, 42, 99} {
		if _, err := db.Exec(`INSERT INTO contest_participants VALUES (?,?,?)`, id, user, start); err != nil {
			t.Fatal(err)
		}
	}
	contest, err := repo.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	// Initial empty leaderboard builds without putting zero-submission users on it.
	if _, total, err := repo.CachedLeaderboard(ctx, contest, 1, 100); err != nil || total != 0 {
		t.Fatalf("cold empty: %d %v", total, err)
	}
	repo.MaintainLeaderboards(ctx)
	if _, total, err := cache.Read(ctx, contest, 1, 100); err != nil || total != 0 {
		t.Fatalf("ready empty: %d %v", total, err)
	}
	facts := make(map[int64]mq.SubmissionJudged)
	for _, user := range []int64{2, 10, 42} {
		fact := mq.SubmissionJudged{SubmissionID: user, ContestID: id, UserID: user, ProblemID: 7, Verdict: "AC", SubmittedAt: start.Add(10 * time.Minute), JudgedAt: start.Add(15 * time.Minute)}
		facts[user] = fact
		if err := repo.ApplyProjection(ctx, biz.ProjectionEvent{EventID: uuid.NewString(), Fact: biz.SubmissionFact{SubmissionJudged: fact}}); err != nil {
			t.Fatal(err)
		}
	}
	relay := &biz.CacheRelay{Repository: repo, Sink: cache}
	drain := func() {
		t.Helper()
		for i := 0; i < 20; i++ {
			claimed, err := relay.RunOnce(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !claimed {
				return
			}
		}
		t.Fatal("Outbox did not drain")
	}
	drain()
	assertPages := func() {
		t.Helper()
		for _, page := range []int32{1, 2, 3} {
			want, total, err := repo.Leaderboard(ctx, id, page, 2)
			if err != nil {
				t.Fatal(err)
			}
			got, cachedTotal, err := cache.Read(ctx, contest, page, 2)
			if err != nil || cachedTotal != total || len(got) != len(want) {
				t.Fatalf("page %d: SQL=%v/%d Redis=%v/%d err=%v", page, want, total, got, cachedTotal, err)
			}
			for i, item := range got {
				if !proto.Equal(item, want[i]) {
					t.Fatalf("page %d row %d mismatch: SQL=%v Redis=%v", page, i, want[i], item)
				}
			}
		}
	}
	assertPages()
	// Register before the consistent read view. Commit changes while streaming;
	// updates and a NEW user must survive later loads of older snapshot rows.
	build, err := cache.BeginBuild(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	changed := false
	captured, err := repo.StreamLeaderboard(ctx, id, func(s biz.LeaderboardSnapshot) error {
		if !changed {
			changed = true
			fact := facts[42]
			fact.Verdict = "WA"
			fact.JudgedAt = fact.JudgedAt.Add(time.Minute)
			if err := repo.ApplyProjection(ctx, biz.ProjectionEvent{EventID: uuid.NewString(), Fact: biz.SubmissionFact{SubmissionJudged: fact}}); err != nil {
				return err
			}
			fact = facts[2]
			fact.UserID = 99
			fact.SubmissionID = 99
			fact.ProblemID = 8
			if err := repo.ApplyProjection(ctx, biz.ProjectionEvent{EventID: uuid.NewString(), Fact: biz.SubmissionFact{SubmissionJudged: fact}}); err != nil {
				return err
			}
			drain()
		}
		return cache.Load(ctx, build, s)
	})
	if err != nil || !changed {
		t.Fatalf("snapshot=%+v changed=%v err=%v", captured, changed, err)
	}
	if err := cache.ActivateBuild(ctx, id, build, contestSignature(captured)); err != nil {
		t.Fatal(err)
	}
	if err := repo.verifyLeaderboard(ctx, contest, build); err != nil {
		t.Fatal(err)
	}
	assertPages()
	// A malformed page with unchanged hash cardinalities must trigger repair.
	cache.client.HSet(ctx, cache.generationKeys(id, build)[1], "2", "bad JSON")
	if _, _, err := cache.Read(ctx, contest, 1, 100); !errors.Is(err, ErrCacheIncomplete) {
		t.Fatal(err)
	}
	if items, total, err := repo.CachedLeaderboard(ctx, contest, 1, 100); err != nil || total != 4 || len(items) != 4 {
		t.Fatalf("SQL repair fallback=%v %d %v", items, total, err)
	}
	repo.leaderboard.mu.Lock()
	repo.leaderboard.contests[id].checkAt = time.Time{}
	repo.leaderboard.contests[id].rebuildAt = time.Time{}
	repo.leaderboard.mu.Unlock()
	repo.MaintainLeaderboards(ctx)
	assertPages()
	// Simulate Redis losing every key after all events have been ACKed. Rebuild
	// reads SQL, so recovery does not require already-confirmed Outbox messages.
	keys, err := cache.client.Keys(ctx, cache.namespace+":*").Result()
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.client.Del(ctx, keys...).Err(); err != nil {
		t.Fatal(err)
	}
	if _, total, err := repo.CachedLeaderboard(ctx, contest, 1, 100); err != nil || total != 4 {
		t.Fatal(total, err)
	}
	repo.leaderboard.mu.Lock()
	repo.leaderboard.contests[id].checkAt = time.Time{}
	repo.leaderboard.contests[id].rebuildAt = time.Time{}
	repo.leaderboard.mu.Unlock()
	repo.MaintainLeaderboards(ctx)
	assertPages()
	active, _, err := cache.pointers(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	// Worker outage / poison event may not falsely renew cache freshness.
	if _, err := db.Exec(`INSERT INTO contest_cache_outbox(event_id,contest_id,user_id,result_version,payload,status,next_retry_at,created_at) VALUES (?,?,2,99,'{}','dead',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6)-INTERVAL 1 MINUTE)`, uuid.NewString(), id); err != nil {
		t.Fatal(err)
	}
	if err := repo.verifyLeaderboard(ctx, contest, active); err != nil {
		t.Fatal(err)
	}
	if _, _, err := cache.Read(ctx, contest, 1, 100); !errors.Is(err, ErrCacheStale) {
		t.Fatalf("outdated board readable despite old dead event: %v", err)
	}
}

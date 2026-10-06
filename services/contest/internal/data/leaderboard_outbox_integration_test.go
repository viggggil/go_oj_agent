package data

import (
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/viggggil/go_oj_agent/pkg/mq"
	"github.com/viggggil/go_oj_agent/services/contest/internal/biz"
	"os"
	"sync"
	"testing"
	"time"
)

func TestMySQLLeaderboardOutbox(t *testing.T) {
	db := testContestDatabase(t)
	repo := NewRepository(db)
	start := time.Now().UTC().Truncate(time.Millisecond).Add(-time.Hour)
	res, err := db.Exec(`INSERT INTO contests (title,status,start_at,end_at,created_by,created_at,updated_at) VALUES ('outbox integration','draft',?,?,1,?,?)`, start, start.Add(2*time.Hour), start, start)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	defer func() {
		for _, table := range []string{"contest_cache_outbox", "contest_user_results", "contest_problem_results", "contest_submission_results", "contest_participants", "contest_problems", "contests"} {
			key := "contest_id"
			if table == "contests" {
				key = "id"
			}
			if _, err := db.Exec("DELETE FROM "+table+" WHERE "+key+"=?", id); err != nil {
				t.Error(err)
			}
		}
	}()
	if _, err := db.Exec(`INSERT INTO contest_participants VALUES (?,42,?)`, id, start); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO contest_problems VALUES (?,7,1,100),(?,8,2,100)`, id, id); err != nil {
		t.Fatal(err)
	}
	base := time.Now().UnixNano()
	fact := mq.SubmissionJudged{SubmissionID: base, ContestID: id, UserID: 42, ProblemID: 7, Verdict: "AC", SubmittedAt: start.Add(10 * time.Minute), JudgedAt: start.Add(20 * time.Minute)}
	event := biz.ProjectionEvent{EventID: uuid.NewString(), Fact: biz.SubmissionFact{SubmissionJudged: fact}}
	if err := repo.ApplyProjection(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	// Same event and a stale new event do not advance the result version.
	if err := repo.ApplyProjection(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	event.EventID = uuid.NewString()
	if err := repo.ApplyProjection(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	assertSummary := func(version int64, solved int32, penalty int64) {
		t.Helper()
		var v, p int64
		var s int32
		if err := db.QueryRow(`SELECT version,solved_count,penalty_seconds FROM contest_user_results WHERE contest_id=? AND user_id=42`, id).Scan(&v, &s, &p); err != nil || v != version || s != solved || p != penalty {
			t.Fatalf("summary=(%d,%d,%d) err=%v", v, s, p, err)
		}
		var n int64
		if err := db.QueryRow(`SELECT COUNT(*) FROM contest_cache_outbox WHERE contest_id=?`, id).Scan(&n); err != nil || n != version {
			t.Fatalf("events=%d version=%d err=%v", n, version, err)
		}
	}
	assertSummary(1, 1, 600)
	// Different events for one user serialize without losing the earlier WA.
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := int64(1); i <= 2; i++ {
		wg.Go(func() {
			f := fact
			f.SubmissionID = base + i
			f.SubmittedAt = start.Add(time.Duration(i) * time.Minute)
			f.Verdict = "WA"
			errs <- repo.ApplyProjection(t.Context(), biz.ProjectionEvent{EventID: uuid.NewString(), Fact: biz.SubmissionFact{SubmissionJudged: f}})
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	assertSummary(3, 1, 3000)
	var payload []byte
	if err := db.QueryRow(`SELECT payload FROM contest_cache_outbox WHERE contest_id=? AND result_version=1`, id).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var snapshot biz.LeaderboardSnapshot
	if err := json.Unmarshal(payload, &snapshot); err != nil || snapshot.Validate() != nil || snapshot.PenaltySeconds != 600 || len(snapshot.Problems) != 2 {
		t.Fatalf("immutable version 1: %+v err=%v", snapshot, err)
	}
	// Force an outbox write failure; event dedup, facts and totals must roll back.
	if _, err := db.Exec(`INSERT INTO contest_cache_outbox(event_id,contest_id,user_id,result_version,payload,next_retry_at,created_at) VALUES (?,?,42,4,'{}',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`, uuid.NewString(), id); err != nil {
		t.Fatal(err)
	}
	f := fact
	f.SubmissionID = base + 3
	f.ProblemID = 8
	failedID := uuid.NewString()
	if err := repo.ApplyProjection(t.Context(), biz.ProjectionEvent{EventID: failedID, Fact: biz.SubmissionFact{SubmissionJudged: f}}); err == nil {
		t.Fatal("expected unique outbox conflict")
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM contest_processed_events WHERE event_id=?`, failedID).Scan(&n); err != nil || n != 0 {
		t.Fatal("dedup record survived rollback", err)
	}
	if _, err := db.Exec(`DELETE FROM contest_cache_outbox WHERE contest_id=? AND result_version=4`, id); err != nil {
		t.Fatal(err)
	}
	assertSummary(3, 1, 3000)
	// Simulate stopping after claim. A new owner recovers expiry; old ACK is fenced.
	token := uuid.NewString()
	claimed, err := repo.ClaimCacheEvent(t.Context(), token, 30*time.Second)
	if err != nil || claimed == nil {
		var pending, leased int
		_ = db.QueryRow(`SELECT COUNT(*),COALESCE(SUM(lease_owner IS NOT NULL),0) FROM contest_cache_outbox WHERE status='pending'`).Scan(&pending, &leased)
		t.Fatalf("claim=%v err=%v pending=%d leased=%d", claimed, err, pending, leased)
	}
	token2 := uuid.NewString()
	other, err := repo.ClaimCacheEvent(t.Context(), token2, 30*time.Second)
	if err != nil || other == nil || other.ID == claimed.ID {
		t.Fatalf("exclusive claim=%v err=%v", other, err)
	}
	if _, err := db.Exec(`UPDATE contest_cache_outbox SET lease_until=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE id=?`, claimed.ID); err != nil {
		t.Fatal(err)
	}
	recovered, err := repo.ClaimCacheEvent(t.Context(), uuid.NewString(), 30*time.Second)
	if err != nil || recovered == nil || recovered.ID != claimed.ID {
		t.Fatalf("reclaim=%v err=%v", recovered, err)
	}
	if !errors.Is(repo.CompleteCacheEvent(t.Context(), claimed.ID, token), biz.ErrLeaseLost) {
		t.Fatal("expired owner acknowledged reclaimed event")
	}
	if err := repo.FailCacheEvent(t.Context(), other.ID, token2, time.Minute, false, "redis offline"); err != nil {
		t.Fatal(err)
	}
	// Simulate old summaries absent after a migration. Rerun initializes once.
	if _, err := db.Exec(`DELETE FROM contest_cache_outbox WHERE contest_id=?`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM contest_user_results WHERE contest_id=?`, id); err != nil {
		t.Fatal(err)
	}
	if count, err := repo.backfillUser(t.Context(), id, 42); err != nil || !count {
		t.Fatalf("backfill=%v err=%v", count, err)
	}
	if count, err := repo.backfillUser(t.Context(), id, 42); err != nil || count {
		t.Fatalf("rerun=%v err=%v", count, err)
	}
	assertSummary(1, 1, 3000)
	if address := os.Getenv("CONTEST_TEST_REDIS_ADDR"); address != "" {
		testMySQLRedisRelay(t, repo, id, address)
	}
}

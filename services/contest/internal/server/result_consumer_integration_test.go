package server

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/viggggil/go_oj_agent/pkg/mq"
	"github.com/viggggil/go_oj_agent/services/contest/internal/biz"
	"github.com/viggggil/go_oj_agent/services/contest/internal/conf"
	"github.com/viggggil/go_oj_agent/services/contest/internal/data"
)

func TestRabbitMySQLProjection(t *testing.T) {
	dsn, url := os.Getenv("CONTEST_TEST_MYSQL_DSN"), os.Getenv("JUDGE_TEST_RABBITMQ_URL")
	if dsn == "" || url == "" {
		t.Skip("set CONTEST_TEST_MYSQL_DSN and JUDGE_TEST_RABBITMQ_URL")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	start := time.Now().UTC().Truncate(time.Millisecond).Add(-time.Hour)
	result, err := db.Exec(`INSERT INTO contests (title,status,start_at,end_at,created_by,created_at,updated_at) VALUES ('projection integration','running',?,?,1,?,?)`, start, start.Add(2*time.Hour), start, start)
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, table := range []string{"contest_problem_results", "contest_submission_results", "contest_participants", "contest_problems", "contests"} {
			key := "contest_id"
			if table == "contests" {
				key = "id"
			}
			if _, err := db.Exec("DELETE FROM "+table+" WHERE "+key+"=?", id); err != nil {
				t.Error(err)
			}
		}
	}()
	if _, err := db.Exec(`INSERT INTO contest_participants VALUES (?,1,?),(?,2,?)`, id, start, id, start); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO contest_problems VALUES (?,7,1,0)`, id); err != nil {
		t.Fatal(err)
	}
	queue := "contest.test." + uuid.NewString()
	cfg := &conf.Bootstrap{Messaging: &conf.MessagingProto{Url: url, Exchange: queue + ".events", Queue: queue, DeadLetterQueue: queue + ".dlq", Prefetch: 2}}
	repo := data.NewRepository(db)
	newServer := func() *ResultConsumerServer {
		s, err := NewResultConsumerServer(cfg, repo)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.Prepare(t.Context()); err != nil {
			t.Fatal(err)
		}
		return s
	}
	conn, err := amqp.Dial(url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	defer ch.Close()
	defer ch.QueueDelete(queue, false, false, false)
	defer ch.QueueDelete(queue+".dlq", false, false, false)
	defer ch.ExchangeDelete(cfg.Messaging.Exchange, false, false)
	if err := ch.Confirm(false); err != nil {
		t.Fatal(err)
	}
	launch := func(s *ResultConsumerServer) chan error {
		done := make(chan error, 1)
		go func() { done <- s.Start(t.Context()) }()
		return done
	}
	stop := func(s *ResultConsumerServer, done chan error) {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		if err := s.Stop(ctx); err != nil {
			t.Fatal(err)
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	s := newServer()
	done := launch(s)
	defer func() {
		if done != nil {
			stop(s, done)
		}
	}()
	publish := func(eventID, typ string, payload any) {
		body, err := mq.MarshalEnvelope(mq.EnvelopeMetadata{EventID: eventID, EventType: typ, EventVersion: 1, OccurredAt: time.Now().UTC()}, payload)
		if err != nil {
			t.Fatal(err)
		}
		confirmation, err := ch.PublishWithDeferredConfirmWithContext(t.Context(), cfg.Messaging.Exchange, typ, false, false, amqp.Publishing{DeliveryMode: amqp.Persistent, Type: typ, Body: body})
		if err != nil {
			t.Fatal(err)
		}
		ok, err := confirmation.WaitContext(t.Context())
		if err != nil || !ok {
			t.Fatalf("confirm=%v err=%v", ok, err)
		}
	}
	wait := func(check func() bool) {
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if check() {
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
		t.Fatal("projection did not converge")
	}
	applied := func(eventID string) {
		wait(func() bool {
			var count int
			err := db.QueryRow(`SELECT COUNT(*) FROM contest_processed_events WHERE consumer_name='contest-result-consumer' AND event_id=?`, eventID).Scan(&count)
			return err == nil && count == 1
		})
	}
	check := func(solved int32, wrong int32, penalty int64) {
		t.Helper()
		items, total, err := repo.Leaderboard(t.Context(), id, 1, 20)
		if err != nil || total != 1 || len(items) != 1 {
			t.Fatalf("leaderboard=%v total=%d err=%v", items, total, err)
		}
		item := items[0]
		if item.SolvedCount != solved || item.PenaltySeconds != penalty || item.Problems[0].WrongAttempts != wrong {
			t.Fatalf("leaderboard=%v", item)
		}
	}
	baseID := time.Now().UnixNano()
	ac := mq.SubmissionJudged{SubmissionID: baseID + 2, ContestID: id, UserID: 1, ProblemID: 7, Verdict: "AC", SubmittedAt: start.Add(10 * time.Minute), JudgedAt: start.Add(20 * time.Minute)}
	wa := ac
	wa.SubmissionID = baseID + 1
	wa.Verdict = "WA"
	wa.SubmittedAt = start.Add(5 * time.Minute)
	acID, waID := uuid.NewString(), uuid.NewString()
	publish(acID, mq.EventSubmissionJudged, ac)
	applied(acID)
	check(1, 0, 600)
	publish(waID, mq.EventSubmissionJudged, wa)
	applied(waID)
	check(1, 1, 1800)
	publish(waID, mq.EventSubmissionJudged, wa)
	marker := uuid.NewString()
	after := wa
	after.SubmissionID = baseID + 3
	after.SubmittedAt = start.Add(15 * time.Minute)
	publish(marker, mq.EventSubmissionJudged, after)
	applied(marker)
	check(1, 1, 1800)
	updated := ac
	updated.Verdict = "WA"
	updated.JudgedAt = ac.JudgedAt.Add(time.Minute)
	updateID := uuid.NewString()
	publish(updateID, mq.EventSubmissionJudged, updated)
	applied(updateID)
	check(0, 3, 0)
	staleID := uuid.NewString()
	publish(staleID, mq.EventSubmissionJudged, ac)
	applied(staleID)
	check(0, 3, 0)
	failure := wa
	failure.SubmissionID = baseID + 4
	failure.Verdict = "SYSTEM_ERROR"
	failure.SubmittedAt = start.Add(3 * time.Minute)
	failureID := uuid.NewString()
	publish(failureID, mq.EventSubmissionJudged, failure)
	applied(failureID)
	check(0, 3, 0)
	// 作废先到，后续 judged 即使时间更晚也不能复活旧 ID。
	invalid := mq.SubmissionInvalidated{SubmissionID: baseID + 5, ContestID: id, UserID: 1, ProblemID: 7, SubmittedAt: start.Add(time.Minute), InvalidatedAt: start.Add(30 * time.Minute)}
	invalidID := uuid.NewString()
	publish(invalidID, mq.EventSubmissionInvalidated, invalid)
	applied(invalidID)
	late := ac
	late.SubmissionID = invalid.SubmissionID
	late.SubmittedAt = invalid.SubmittedAt
	late.JudgedAt = start.Add(40 * time.Minute)
	lateID := uuid.NewString()
	publish(lateID, mq.EventSubmissionJudged, late)
	applied(lateID)
	check(0, 3, 0)
	// 已有结果作废后局部重算，WA 也不再计分。
	invalid.SubmissionID = wa.SubmissionID
	invalid.SubmittedAt = wa.SubmittedAt
	invalidID = uuid.NewString()
	publish(invalidID, mq.EventSubmissionInvalidated, invalid)
	applied(invalidID)
	check(0, 2, 0)
	// 停机时投递，并模拟未 ACK 消息在连接关闭后再次投递。
	wait(func() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.cancel != nil })
	stop(s, done)
	done = nil
	replacement := ac
	replacement.SubmissionID = baseID + 6
	replacement.SubmittedAt = start.Add(25 * time.Minute)
	replacementID := uuid.NewString()
	publish(replacementID, mq.EventSubmissionJudged, replacement)
	redeliveryCh, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	delivery, ok, err := redeliveryCh.Get(queue, false)
	if err != nil || !ok {
		t.Fatalf("get unacked delivery=%v err=%v", ok, err)
	}
	if delivery.MessageCount != 0 {
		t.Fatal("expected only restart delivery")
	}
	if err := redeliveryCh.Close(); err != nil {
		t.Fatal(err)
	}
	s = newServer()
	done = launch(s)
	applied(replacementID)
	check(1, 2, 3900)
	// 无效协议进入 DLQ；本地归属失败不提交 event 去重记录。
	badID := uuid.NewString()
	bad := ac
	bad.Verdict = "UNKNOWN"
	publish(badID, mq.EventSubmissionJudged, bad)
	wait(func() bool { q, err := ch.QueueInspect(queue + ".dlq"); return err == nil && q.Messages > 0 })
	missing := ac
	missing.UserID = 999
	missing.SubmissionID = baseID + 7
	missingID := uuid.NewString()
	publish(missingID, mq.EventSubmissionJudged, missing)
	time.Sleep(250 * time.Millisecond)
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM contest_processed_events WHERE event_id=?`, missingID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("invalid membership committed count=%d err=%v", count, err)
	}
	stop(s, done)
	done = nil
	// 排序与分页在相同成绩下以 user_id 稳定排序。
	second := replacement
	second.UserID = 2
	second.SubmissionID = baseID + 8
	second.SubmittedAt = start.Add(65 * time.Minute)
	if err := repo.ApplyProjection(t.Context(), biz.ProjectionEvent{EventID: uuid.NewString(), Fact: biz.SubmissionFact{SubmissionJudged: second}}); err != nil {
		t.Fatal(err)
	}
	items, total, err := repo.Leaderboard(t.Context(), id, 2, 1)
	if err != nil || total != 2 || len(items) != 1 || items[0].UserId != 2 || items[0].Rank != 2 {
		t.Fatalf("pagination=%v total=%d err=%v", items, total, err)
	}
	// 两个合法参与者竞争同一个新 Submission ID，不允许跨用户覆盖身份或留下错误投影。
	conflict := replacement
	conflict.SubmissionID = baseID + 9
	other := conflict
	other.UserID = 2
	other.Verdict = "WA"
	begin := make(chan struct{})
	results := make(chan error, 2)
	for _, fact := range []mq.SubmissionJudged{conflict, other} {
		go func(fact mq.SubmissionJudged) {
			<-begin
			results <- repo.ApplyProjection(t.Context(), biz.ProjectionEvent{EventID: uuid.NewString(), Fact: biz.SubmissionFact{SubmissionJudged: fact}})
		}(fact)
	}
	close(begin)
	failures := 0
	for i := 0; i < 2; i++ {
		if <-results != nil {
			failures++
		}
	}
	if failures != 1 {
		t.Fatalf("conflicting identity failures=%d", failures)
	}
	var storedUser int64
	var storedVerdict string
	if err := db.QueryRow(`SELECT user_id,verdict FROM contest_submission_results WHERE submission_id=?`, conflict.SubmissionID).Scan(&storedUser, &storedVerdict); err != nil {
		t.Fatal(err)
	}
	if (storedUser == 1 && storedVerdict != "AC") || (storedUser == 2 && storedVerdict != "WA") {
		t.Fatalf("corrupted identity user=%d verdict=%s", storedUser, storedVerdict)
	}
}

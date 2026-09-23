package data

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/viggggil/go_oj_agent/services/judge/internal/biz"
)

func TestStoreClaimOutboxLeasesRows(t *testing.T) {
	store, mock, now := newMockStore(t)
	leaseUntil := now.Add(time.Minute)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, event_id, aggregate_type, aggregate_id, event_type").
		WithArgs(biz.OutboxStatusPending, now, now, 2).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "event_id", "aggregate_type", "aggregate_id", "event_type", "event_version", "payload", "status", "retry_count", "next_retry_at", "lease_owner", "lease_until", "created_at", "published_at",
		}).AddRow(1, "123e4567-e89b-12d3-a456-426614174000", "submission", 9, biz.EventTypeJudgeRequested, 1, []byte(`{"submission_id":9}`), biz.OutboxStatusPending, 0, nil, nil, nil, now, nil)).
		RowsWillBeClosed()
	mock.ExpectExec("UPDATE outbox_events").
		WithArgs("relay-1", leaseUntil, int64(1), biz.OutboxStatusPending).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	events, err := store.ClaimOutbox(context.Background(), "relay-1", now, leaseUntil, 2)
	if err != nil || len(events) != 1 || events[0].LeaseOwner != "relay-1" || events[0].LeaseUntil == nil {
		t.Fatalf("ClaimOutbox() = %+v, %v", events, err)
	}
	if string(events[0].Payload) != `{"submission_id":9}` {
		t.Fatalf("payload = %s", events[0].Payload)
	}
	assertExpectations(t, mock)
}

func TestStoreOutboxPublishAndFailureUpdatesRequireLeaseOwner(t *testing.T) {
	store, mock, now := newMockStore(t)
	mock.ExpectExec("UPDATE outbox_events").
		WithArgs(biz.OutboxStatusPublished, now, int64(1), biz.OutboxStatusPending, "relay-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := store.MarkOutboxPublished(context.Background(), 1, "relay-1", now); err != nil {
		t.Fatalf("MarkOutboxPublished() error = %v", err)
	}
	next := now.Add(5 * time.Second)
	mock.ExpectExec("UPDATE outbox_events").
		WithArgs(biz.OutboxStatusPending, next, "temporary failure", int64(1), biz.OutboxStatusPending, "relay-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := store.MarkOutboxFailure(context.Background(), 1, "relay-1", next, false, "temporary failure"); err != nil {
		t.Fatalf("MarkOutboxFailure() error = %v", err)
	}
	assertExpectations(t, mock)
}

func TestOutboxPayloadRoundTripForRepositoryTest(t *testing.T) {
	payload, err := json.Marshal(biz.JudgeRequestedPayload{SubmissionID: 1})
	if err != nil || len(payload) == 0 {
		t.Fatal(err)
	}
	if matched, _ := regexp.Match(`submission_id`, payload); !matched {
		t.Fatalf("payload = %s", payload)
	}
}

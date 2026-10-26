package data

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
)

func TestCacheOutboxObservationFailureRetainsSnapshot(t *testing.T) {
	previous := CacheMetricsSnapshot()
	outboxObservationMu.Lock()
	leaderboardOutboxBacklog.Set(9)
	leaderboardOutboxPending.Set(7)
	leaderboardOutboxOldest.Set(123456)
	leaderboardOutboxDead.Set(2)
	leaderboardObservedAt.Set(123)
	outboxObservationMu.Unlock()
	defer func() {
		outboxObservationMu.Lock()
		leaderboardOutboxBacklog.Set(previous["outbox_backlog"])
		leaderboardOutboxPending.Set(previous["outbox_pending"])
		leaderboardOutboxOldest.Set(previous["outbox_oldest_microseconds"])
		leaderboardOutboxDead.Set(previous["outbox_dead"])
		leaderboardObservedAt.Set(previous["outbox_observed_at_seconds"])
		outboxObservationMu.Unlock()
	}()

	db := sql.OpenDB(unavailableMetricsConnector{})
	defer db.Close()
	before := CacheMetricsSnapshot()
	failuresBefore := leaderboardObservationErrors.Value()
	(&Repository{db: db}).ObserveCacheOutbox(context.Background())
	after := CacheMetricsSnapshot()
	for _, key := range []string{"outbox_backlog", "outbox_pending", "outbox_oldest_microseconds", "outbox_dead", "outbox_observed_at_seconds"} {
		if after[key] != before[key] {
			t.Fatalf("failed observation replaced %s: before=%d after=%d", key, before[key], after[key])
		}
	}
	if got := leaderboardObservationErrors.Value(); got != failuresBefore+1 {
		t.Fatalf("observation failures = %d, want %d", got, failuresBefore+1)
	}
}

type unavailableMetricsConnector struct{}

func (unavailableMetricsConnector) Connect(context.Context) (driver.Conn, error) {
	return unavailableMetricsConn{}, nil
}
func (unavailableMetricsConnector) Driver() driver.Driver { return unavailableMetricsDriver{} }

type unavailableMetricsDriver struct{}

func (unavailableMetricsDriver) Open(string) (driver.Conn, error) {
	return unavailableMetricsConn{}, nil
}

type unavailableMetricsConn struct{}

func (unavailableMetricsConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("observation database unavailable")
}
func (unavailableMetricsConn) Close() error              { return nil }
func (unavailableMetricsConn) Begin() (driver.Tx, error) { return nil, errors.New("unsupported") }
func (unavailableMetricsConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return nil, errors.New("observation database unavailable")
}

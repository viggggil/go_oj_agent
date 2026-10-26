package data

import (
	"context"
	"expvar"
	"fmt"
	"sync"
	"time"
)

// 进程级低基数观测；运维端口以 Prometheus 文本和 JSON 暴露这些值。
var (
	outboxObservationMu            sync.RWMutex
	leaderboardCacheHits           = expvar.NewInt("contest_leaderboard_cache_hits")
	leaderboardCacheMisses         = expvar.NewInt("contest_leaderboard_cache_misses")
	leaderboardCacheErrors         = expvar.NewInt("contest_leaderboard_cache_errors")
	leaderboardCacheRebuilds       = expvar.NewInt("contest_leaderboard_cache_rebuilds")
	leaderboardCacheFailures       = expvar.NewInt("contest_leaderboard_cache_rebuild_failures")
	leaderboardOutboxBacklog       = expvar.NewInt("contest_leaderboard_outbox_backlog")
	leaderboardOutboxPending       = expvar.NewInt("contest_leaderboard_cache_outbox_pending")
	leaderboardOutboxFailures      = expvar.NewInt("contest_leaderboard_outbox_failures")
	leaderboardSQLFallbacks        = expvar.NewInt("contest_leaderboard_sql_fallbacks")
	leaderboardOutboxOldest        = expvar.NewInt("contest_leaderboard_outbox_oldest_microseconds")
	leaderboardOutboxDead          = expvar.NewInt("contest_leaderboard_outbox_dead")
	leaderboardOutboxApplied       = expvar.NewInt("contest_leaderboard_outbox_applied")
	leaderboardUpdateMicros        = expvar.NewInt("contest_leaderboard_update_microseconds_total")
	leaderboardRebuildMicros       = expvar.NewInt("contest_leaderboard_rebuild_microseconds_total")
	leaderboardTransactionRetries  = expvar.NewInt("contest_leaderboard_transaction_retries")
	leaderboardObservationErrors   = expvar.NewInt("contest_leaderboard_observation_errors")
	leaderboardObservedAt          = expvar.NewInt("contest_leaderboard_outbox_observed_at_seconds")
	leaderboardOutboxRetry         = expvar.NewInt("contest_leaderboard_outbox_retries")
	leaderboardOutboxLeaseFailures = expvar.NewInt("contest_leaderboard_outbox_lease_failures")
)

var projectionDurationBounds = []time.Duration{10 * time.Millisecond, 50 * time.Millisecond, 100 * time.Millisecond, 500 * time.Millisecond, time.Second, 5 * time.Second}

type projectionDurationMetrics struct {
	count     *expvar.Int
	sumMicros *expvar.Int
	buckets   []*expvar.Int
}

var (
	projectionMetricsMu       sync.RWMutex
	projectionSuccess         = expvar.NewInt("contest_projection_success_total")
	projectionFailure         = expvar.NewInt("contest_projection_failure_total")
	projectionProcessedEvents = expvar.NewInt("contest_projection_processed_events_total")
	projectionDurations       = [2]projectionDurationMetrics{
		newProjectionDurationMetrics("success"), newProjectionDurationMetrics("error"),
	}
)

func newProjectionDurationMetrics(outcome string) projectionDurationMetrics {
	metrics := projectionDurationMetrics{
		count:     expvar.NewInt("contest_projection_duration_" + outcome + "_count"),
		sumMicros: expvar.NewInt("contest_projection_duration_" + outcome + "_sum_microseconds"),
		buckets:   make([]*expvar.Int, len(projectionDurationBounds)+1),
	}
	for i := range metrics.buckets {
		metrics.buckets[i] = expvar.NewInt(fmt.Sprintf("contest_projection_duration_%s_bucket_%d", outcome, i))
	}
	return metrics
}

func recordCacheHit()   { leaderboardCacheHits.Add(1) }
func recordCacheMiss()  { leaderboardCacheMisses.Add(1) }
func recordCacheError() { leaderboardCacheErrors.Add(1) }

// 独立刷新 pending/dead 积压；查询失败不影响成绩事务，保留上次值并记录观测故障。
func (r *Repository) ObserveCacheOutbox(ctx context.Context) {
	if r == nil || r.db == nil {
		return
	}
	var backlog, pending, oldest, dead int64
	if err := r.db.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(status IN ('pending','dead')),0),
		COALESCE(SUM(status='pending'),0),
		COALESCE(GREATEST(TIMESTAMPDIFF(MICROSECOND,MIN(CASE WHEN status='pending' THEN created_at END),UTC_TIMESTAMP(6)),0),0),
		COALESCE(SUM(status='dead'),0)
		FROM contest_cache_outbox WHERE status IN ('pending','dead')`).Scan(&backlog, &pending, &oldest, &dead); err != nil {
		leaderboardObservationErrors.Add(1)
		return
	}
	outboxObservationMu.Lock()
	defer outboxObservationMu.Unlock()
	leaderboardOutboxBacklog.Set(backlog)
	leaderboardOutboxPending.Set(pending)
	leaderboardOutboxOldest.Set(oldest)
	leaderboardOutboxDead.Set(dead)
	leaderboardObservedAt.Set(time.Now().Unix())
}

func RecordCacheRelayOutcome(outcome string, age time.Duration) {
	switch outcome {
	case "applied":
		leaderboardOutboxApplied.Add(1)
		leaderboardUpdateMicros.Add(age.Microseconds())
	case "retry":
		leaderboardOutboxRetry.Add(1)
		leaderboardOutboxFailures.Add(1)
	case "lease_lost":
		leaderboardOutboxLeaseFailures.Add(1)
		leaderboardOutboxFailures.Add(1)
	case "dead", "error", "confirm_error":
		leaderboardOutboxFailures.Add(1)
	}
}

func RecordProjectionResult(err error, duration time.Duration) {
	projectionMetricsMu.Lock()
	defer projectionMetricsMu.Unlock()
	metrics := &projectionDurations[0]
	if err != nil {
		metrics = &projectionDurations[1]
		projectionFailure.Add(1)
	} else {
		projectionSuccess.Add(1)
	}
	if duration < 0 {
		duration = 0
	}
	metrics.count.Add(1)
	metrics.sumMicros.Add(duration.Microseconds())
	for i, bound := range projectionDurationBounds {
		if duration <= bound {
			metrics.buckets[i].Add(1)
		}
	}
	metrics.buckets[len(projectionDurationBounds)].Add(1)
}

func RecordProjectionEventProcessed() { projectionProcessedEvents.Add(1) }

func CacheMetricsSnapshot() map[string]int64 {
	outboxObservationMu.RLock()
	backlog := leaderboardOutboxBacklog.Value()
	pending := leaderboardOutboxPending.Value()
	oldest := leaderboardOutboxOldest.Value()
	dead := leaderboardOutboxDead.Value()
	observedAt := leaderboardObservedAt.Value()
	outboxObservationMu.RUnlock()
	return map[string]int64{
		"cache_hits":                  leaderboardCacheHits.Value(),
		"cache_misses":                leaderboardCacheMisses.Value(),
		"cache_errors":                leaderboardCacheErrors.Value(),
		"rebuilds":                    leaderboardCacheRebuilds.Value(),
		"rebuild_failures":            leaderboardCacheFailures.Value(),
		"outbox_backlog":              backlog,
		"outbox_pending":              pending,
		"outbox_failures":             leaderboardOutboxFailures.Value(),
		"outbox_applied":              leaderboardOutboxApplied.Value(),
		"sql_fallbacks":               leaderboardSQLFallbacks.Value(),
		"outbox_oldest_microseconds":  oldest,
		"outbox_dead":                 dead,
		"update_microseconds_total":   leaderboardUpdateMicros.Value(),
		"rebuild_microseconds_total":  leaderboardRebuildMicros.Value(),
		"transaction_retries":         leaderboardTransactionRetries.Value(),
		"observation_errors":          leaderboardObservationErrors.Value(),
		"outbox_observed_at_seconds":  observedAt,
		"outbox_retries":              leaderboardOutboxRetry.Value(),
		"outbox_lease_failures":       leaderboardOutboxLeaseFailures.Value(),
		"projection_success":          projectionSuccess.Value(),
		"projection_failure":          projectionFailure.Value(),
		"projection_processed_events": projectionProcessedEvents.Value(),
	}
}

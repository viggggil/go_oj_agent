package data

import (
	"context"
	"expvar"
	"time"
)

// 进程级低基数观测；运维端口以 Prometheus 文本和 JSON 暴露这些值。
var (
	leaderboardCacheHits          = expvar.NewInt("contest_leaderboard_cache_hits")
	leaderboardCacheMisses        = expvar.NewInt("contest_leaderboard_cache_misses")
	leaderboardCacheErrors        = expvar.NewInt("contest_leaderboard_cache_errors")
	leaderboardCacheRebuilds      = expvar.NewInt("contest_leaderboard_cache_rebuilds")
	leaderboardCacheFailures      = expvar.NewInt("contest_leaderboard_cache_rebuild_failures")
	leaderboardOutboxBacklog      = expvar.NewInt("contest_leaderboard_outbox_backlog")
	leaderboardOutboxFailures     = expvar.NewInt("contest_leaderboard_outbox_failures")
	leaderboardSQLFallbacks       = expvar.NewInt("contest_leaderboard_sql_fallbacks")
	leaderboardOutboxOldest       = expvar.NewInt("contest_leaderboard_outbox_oldest_microseconds")
	leaderboardOutboxDead         = expvar.NewInt("contest_leaderboard_outbox_dead")
	leaderboardOutboxApplied      = expvar.NewInt("contest_leaderboard_outbox_applied")
	leaderboardUpdateMicros       = expvar.NewInt("contest_leaderboard_update_microseconds_total")
	leaderboardRebuildMicros      = expvar.NewInt("contest_leaderboard_rebuild_microseconds_total")
	leaderboardTransactionRetries = expvar.NewInt("contest_leaderboard_transaction_retries")
	leaderboardObservationErrors  = expvar.NewInt("contest_leaderboard_observation_errors")
	leaderboardObservedAt         = expvar.NewInt("contest_leaderboard_outbox_observed_at_seconds")
)

func recordCacheHit()   { leaderboardCacheHits.Add(1) }
func recordCacheMiss()  { leaderboardCacheMisses.Add(1) }
func recordCacheError() { leaderboardCacheErrors.Add(1) }

// 独立刷新 pending/dead 积压；查询失败不影响成绩事务，保留上次值并记录观测故障。
func (r *Repository) ObserveCacheOutbox(ctx context.Context) {
	if r == nil || r.db == nil {
		return
	}
	var backlog, oldest, dead int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(GREATEST(TIMESTAMPDIFF(MICROSECOND,MIN(created_at),UTC_TIMESTAMP(6)),0),0),COALESCE(SUM(status='dead'),0) FROM contest_cache_outbox WHERE status IN ('pending','dead')`).Scan(&backlog, &oldest, &dead); err != nil {
		leaderboardObservationErrors.Add(1)
		return
	}
	leaderboardOutboxBacklog.Set(backlog)
	leaderboardOutboxOldest.Set(oldest)
	leaderboardOutboxDead.Set(dead)
	leaderboardObservedAt.Set(time.Now().Unix())
}

func RecordCacheRelayOutcome(outcome string, age time.Duration) {
	switch outcome {
	case "applied":
		leaderboardOutboxApplied.Add(1)
		leaderboardUpdateMicros.Add(age.Microseconds())
	case "retry", "dead", "error", "confirm_error":
		leaderboardOutboxFailures.Add(1)
	}
}

func CacheMetricsSnapshot() map[string]int64 {
	return map[string]int64{
		"cache_hits":                 leaderboardCacheHits.Value(),
		"cache_misses":               leaderboardCacheMisses.Value(),
		"cache_errors":               leaderboardCacheErrors.Value(),
		"rebuilds":                   leaderboardCacheRebuilds.Value(),
		"rebuild_failures":           leaderboardCacheFailures.Value(),
		"outbox_backlog":             leaderboardOutboxBacklog.Value(),
		"outbox_failures":            leaderboardOutboxFailures.Value(),
		"outbox_applied":             leaderboardOutboxApplied.Value(),
		"sql_fallbacks":              leaderboardSQLFallbacks.Value(),
		"outbox_oldest_microseconds": leaderboardOutboxOldest.Value(),
		"outbox_dead":                leaderboardOutboxDead.Value(),
		"update_microseconds_total":  leaderboardUpdateMicros.Value(),
		"rebuild_microseconds_total": leaderboardRebuildMicros.Value(),
		"transaction_retries":        leaderboardTransactionRetries.Value(),
		"observation_errors":         leaderboardObservationErrors.Value(),
		"outbox_observed_at_seconds": leaderboardObservedAt.Value(),
	}
}

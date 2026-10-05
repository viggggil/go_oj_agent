package data

import (
	"context"
	"expvar"
)

// These process-wide counters are intentionally dependency-free. A process
// embedding the standard expvar handler can expose them at /debug/vars.
var (
	leaderboardCacheHits      = expvar.NewInt("contest_leaderboard_cache_hits")
	leaderboardCacheMisses    = expvar.NewInt("contest_leaderboard_cache_misses")
	leaderboardCacheErrors    = expvar.NewInt("contest_leaderboard_cache_errors")
	leaderboardCacheRebuilds  = expvar.NewInt("contest_leaderboard_cache_rebuilds")
	leaderboardCacheFailures  = expvar.NewInt("contest_leaderboard_cache_rebuild_failures")
	leaderboardOutboxBacklog  = expvar.NewInt("contest_leaderboard_outbox_backlog")
	leaderboardOutboxFailures = expvar.NewInt("contest_leaderboard_outbox_failures")
)

func recordCacheHit()   { leaderboardCacheHits.Add(1) }
func recordCacheMiss()  { leaderboardCacheMisses.Add(1) }
func recordCacheError() { leaderboardCacheErrors.Add(1) }

// ObserveCacheOutbox refreshes the current pending/dead queue size without
// making it part of the result or cache transaction. A temporary metrics query
// failure is ignored because observability must never block leaderboard work.
func (r *Repository) ObserveCacheOutbox(ctx context.Context) {
	if r == nil || r.db == nil {
		return
	}
	var backlog int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM contest_cache_outbox WHERE status IN ('pending','dead')`).Scan(&backlog); err != nil {
		return
	}
	leaderboardOutboxBacklog.Set(backlog)
}

func RecordCacheRelayFailure() { leaderboardOutboxFailures.Add(1) }

func CacheMetricsSnapshot() map[string]int64 {
	return map[string]int64{
		"cache_hits":       leaderboardCacheHits.Value(),
		"cache_misses":     leaderboardCacheMisses.Value(),
		"cache_errors":     leaderboardCacheErrors.Value(),
		"rebuilds":         leaderboardCacheRebuilds.Value(),
		"rebuild_failures": leaderboardCacheFailures.Value(),
		"outbox_backlog":   leaderboardOutboxBacklog.Value(),
		"outbox_failures":  leaderboardOutboxFailures.Value(),
	}
}

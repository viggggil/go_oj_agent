package data

import (
	"fmt"
	"strings"
)

// PrometheusMetrics renders the process-local expvar counters and periodically
// refreshed Outbox snapshot with stable low-cardinality names. /debug/vars
// remains available with its legacy keys for existing operators.
func PrometheusMetrics() string {
	values := CacheMetricsSnapshot()
	var out strings.Builder
	writeCounter := func(name, help string, value int64) {
		fmt.Fprintf(&out, "# HELP %s %s\n# TYPE %s counter\n%s %d\n", name, help, name, name, value)
	}
	writeFloatCounter := func(name, help string, value float64) {
		fmt.Fprintf(&out, "# HELP %s %s\n# TYPE %s counter\n%s %g\n", name, help, name, name, value)
	}
	writeGauge := func(name, help string, value float64) {
		fmt.Fprintf(&out, "# HELP %s %s\n# TYPE %s gauge\n%s %g\n", name, help, name, name, value)
	}
	for _, metric := range []struct {
		name, help, key string
	}{
		{"oj_contest_cache_hit_total", "Contest leaderboard cache hits", "cache_hits"},
		{"oj_contest_cache_miss_total", "Contest leaderboard cache misses", "cache_misses"},
		{"oj_contest_cache_error_total", "Contest leaderboard cache errors", "cache_errors"},
		{"oj_contest_cache_rebuild_total", "Contest leaderboard cache rebuild attempts", "rebuilds"},
		{"oj_contest_cache_rebuild_failure_total", "Contest leaderboard cache rebuild failures", "rebuild_failures"},
		{"oj_contest_cache_sql_fallback_total", "Contest leaderboard SQL fallback reads", "sql_fallbacks"},
		{"oj_contest_cache_outbox_apply_total", "Contest leaderboard cache Outbox applications", "outbox_applied"},
		{"oj_contest_cache_outbox_retry_total", "Contest leaderboard cache Outbox retries", "outbox_retries"},
		{"oj_contest_cache_outbox_lease_failure_total", "Contest cache Outbox lease fencing failures", "outbox_lease_failures"},
		{"oj_contest_cache_outbox_failure_total", "Contest leaderboard cache Outbox failures", "outbox_failures"},
		{"oj_contest_cache_outbox_observation_failure_total", "Contest cache Outbox observation query failures", "observation_errors"},
		{"oj_contest_cache_transaction_retry_total", "Contest projection transaction retries", "transaction_retries"},
	} {
		writeCounter(metric.name, metric.help, values[metric.key])
	}
	writeFloatCounter("oj_contest_cache_outbox_delivery_age_seconds_total", "Total age from Contest cache Outbox creation through successful cache application", float64(values["update_microseconds_total"])/1_000_000)
	writeFloatCounter("oj_contest_cache_rebuild_duration_seconds_total", "Total duration spent rebuilding Contest leaderboard cache", float64(values["rebuild_microseconds_total"])/1_000_000)
	writeGauge("oj_contest_cache_outbox_backlog", "Pending and dead Contest cache Outbox rows from the last successful observation", float64(values["outbox_backlog"]))
	writeGauge("oj_contest_cache_outbox_pending", "Pending Contest cache Outbox rows from the last successful observation", float64(values["outbox_pending"]))
	writeGauge("oj_contest_cache_outbox_oldest_age_seconds", "Age of the oldest pending Contest cache Outbox row at the last successful observation", float64(values["outbox_oldest_microseconds"])/1_000_000)
	writeGauge("oj_contest_cache_outbox_dead", "Dead Contest cache Outbox rows from the last successful observation", float64(values["outbox_dead"]))
	writeGauge("oj_contest_cache_outbox_observed_at_seconds", "Unix timestamp of the last successful Contest cache Outbox observation", float64(values["outbox_observed_at_seconds"]))
	writeProjectionMetrics(&out)
	return out.String()
}

func writeProjectionMetrics(out *strings.Builder) {
	projectionMetricsMu.RLock()
	defer projectionMetricsMu.RUnlock()
	fmt.Fprintln(out, "# HELP oj_contest_projection_total Contest result projection transactions by outcome")
	fmt.Fprintln(out, "# TYPE oj_contest_projection_total counter")
	for i, outcome := range []string{"success", "error"} {
		count := projectionSuccess.Value()
		if i == 1 {
			count = projectionFailure.Value()
		}
		fmt.Fprintf(out, "oj_contest_projection_total{outcome=\"%s\"} %d\n", outcome, count)
	}
	fmt.Fprintf(out, "# HELP oj_contest_projection_processed_event_total Distinct Contest event IDs committed to processed_events\n# TYPE oj_contest_projection_processed_event_total counter\noj_contest_projection_processed_event_total %d\n", projectionProcessedEvents.Value())
	fmt.Fprintln(out, "# HELP oj_contest_projection_duration_seconds Duration of Contest result projection transactions")
	fmt.Fprintln(out, "# TYPE oj_contest_projection_duration_seconds histogram")
	for i, outcome := range []string{"success", "error"} {
		metrics := projectionDurations[i]
		for bucket, bound := range projectionDurationBounds {
			fmt.Fprintf(out, "oj_contest_projection_duration_seconds_bucket{outcome=\"%s\",le=\"%g\"} %d\n", outcome, bound.Seconds(), metrics.buckets[bucket].Value())
		}
		fmt.Fprintf(out, "oj_contest_projection_duration_seconds_bucket{outcome=\"%s\",le=\"+Inf\"} %d\n", outcome, metrics.buckets[len(projectionDurationBounds)].Value())
		fmt.Fprintf(out, "oj_contest_projection_duration_seconds_sum{outcome=\"%s\"} %g\n", outcome, float64(metrics.sumMicros.Value())/1_000_000)
		fmt.Fprintf(out, "oj_contest_projection_duration_seconds_count{outcome=\"%s\"} %d\n", outcome, metrics.count.Value())
	}
}

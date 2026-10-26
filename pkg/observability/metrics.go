package observability

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
)

// InstrumentationName is shared by Go services. The service identity belongs
// to the OTel resource, rather than being repeated as a high-cardinality
// metric label.
const InstrumentationName = "oj.observability"

const (
	LabelService     = "service"
	LabelEnvironment = "environment"
	LabelRoute       = "route"
	LabelRPC         = "rpc"
	LabelStatus      = "status"
	LabelOutcome     = "outcome"
	LabelVerdict     = "verdict"
	LabelLanguage    = "language"
	LabelPurpose     = "purpose"
	LabelModel       = "model"
	LabelTool        = "tool"
	LabelErrorCode   = "error_code"
	LabelUsageSource = "usage_source"
)

const maxMetricLabelValueLength = 64

var (
	metricLabelValuePattern = regexp.MustCompile(`^[a-z0-9/_:.+{}-][a-z0-9._:/+{}-]{0,63}$`)
	metricUUIDPattern       = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	metricNumericPathID     = regexp.MustCompile(`(?:^|/)[0-9]+(?:/|$)`)
	metricULIDPattern       = regexp.MustCompile(`^[0-9a-hjkmnp-tv-z]{26}$`)
	allowedMetricLabels     = map[string]struct{}{
		LabelService: {}, LabelEnvironment: {}, LabelRoute: {}, LabelRPC: {},
		LabelStatus: {}, LabelOutcome: {}, LabelVerdict: {}, LabelLanguage: {},
		LabelPurpose: {}, LabelModel: {}, LabelTool: {}, LabelErrorCode: {},
		LabelUsageSource: {},
	}
	controlledMetricValues = map[string]map[string]struct{}{
		LabelService: {
			"agent-rag-worker": {}, "agent-service": {}, "contest-service": {},
			"gateway-service": {}, "judge-service": {}, "judge-worker": {},
			"problem-service": {}, "other": {},
		},
		LabelEnvironment: {"development": {}, "production": {}, "staging": {}, "test": {}, "other": {}},
		LabelRoute: {
			"/healthz": {}, "/api/v1/agent/chat": {}, "/api/v1/users/me": {},
			"/api/v1/users/{id}": {}, "/api/v1/problems": {},
			"/api/v1/problems/{problem_id}":                         {},
			"/api/v1/problems/{problem_id}/testcases":               {},
			"/api/v1/problems/{problem_id}/testcases/{testcase_id}": {},
			"/api/v1/contests":                                      {}, "/api/v1/contests/{contest_id}": {},
			"/api/v1/contests/{contest_id}/leaderboard":                       {},
			"/api/v1/contests/{contest_id}/join":                              {},
			"/api/v1/contests/{contest_id}/problems/{problem_id}/submissions": {},
			"/api/v1/submissions":                                             {}, "/api/v1/submissions/{submission_id}": {},
			"/api/v1/submissions/{submission_id}/source":  {},
			"/api/v1/submissions/{submission_id}/result":  {},
			"/api/v1/submissions/{submission_id}/rejudge": {},
			"/api/v1/auth/register":                       {}, "/api/v1/auth/login": {},
			"/api/v1/auth/refresh": {}, "/api/v1/auth/logout": {}, "other": {},
		},
		LabelRPC: {
			"gateway.v1.gatewayservice/createsubmission": {},
			"judge.v1.judgeservice/createsubmission":     {},
			"problem.v1.problemservice/getproblem":       {},
			"contest.v1.contestservice/getleaderboard":   {}, "other": {},
		},
		LabelStatus: {
			"completed": {}, "failed": {}, "cancelled": {}, "timeout": {},
			"retrying": {}, "running": {}, "success": {}, "error": {}, "other": {},
		},
		LabelOutcome:  {"success": {}, "error": {}, "timeout": {}, "cancelled": {}, "retrying": {}, "dropped": {}, "other": {}},
		LabelVerdict:  {"ac": {}, "wa": {}, "ce": {}, "tle": {}, "mle": {}, "re": {}, "system_error": {}, "unspecified": {}, "other": {}},
		LabelLanguage: {"c": {}, "cpp": {}, "go": {}, "java": {}, "python": {}, "rust": {}, "other": {}},
		LabelPurpose:  {"chat": {}, "memory": {}, "eval": {}, "review": {}, "rag_ingestion": {}, "other": {}},
		LabelModel:    {"fake": {}, "deepseek-v4-flash": {}, "deepseek-v4-pro": {}, "other": {}},
		LabelTool: {
			"get_problem": {}, "list_problems": {}, "get_submission": {},
			"get_submission_source": {}, "get_judge_result": {},
			"list_submissions": {}, "run_code": {}, "other": {},
		},
		LabelErrorCode: {
			"agent_model_timeout": {}, "agent_model_unavailable": {},
			"agent_model_rate_limit": {}, "agent_model_rejected": {},
			"agent_model_auth_failed": {}, "agent_model_quota": {},
			"agent_model_blocked": {}, "agent_model_invalid_response": {},
			"agent_model_output_limit": {}, "agent_model_input_limit": {},
			"agent_model_call_limit": {}, "agent_model_unsupported_output": {},
			"agent_credential_unavailable": {}, "agent_run_failed": {},
			"agent_background_failed": {}, "otel_export_failed": {},
			"otel_init_failed": {}, "otel_shutdown_failed": {},
			"collector_unavailable": {}, "queue_full": {}, "other": {},
		},
		LabelUsageSource: {"provider": {}, "estimated": {}, "other": {}},
	}
)

// Label is an explicitly allowlisted metric dimension. Callers must use
// stable route/RPC templates and controlled enum values, never request or
// business identifiers.
type Label struct {
	Key   string
	Value string
}

// Meter returns the shared Go instrumentation meter. Service and environment
// are attached to the resource by Init and must not be supplied as per-request
// labels.
func Meter() otelmetric.Meter {
	return otel.Meter(InstrumentationName)
}

// Attributes normalizes and allowlists metric labels. Invalid keys are
// dropped; invalid values become "other" so a caller cannot accidentally
// create an unbounded series or leak an identifier into Prometheus.
func Attributes(labels ...Label) []attribute.KeyValue {
	values := make(map[string]string, len(labels))
	for _, label := range labels {
		key := strings.TrimSpace(strings.ToLower(label.Key))
		if _, ok := allowedMetricLabels[key]; !ok {
			continue
		}
		value := normalizeMetricLabelValue(key, label.Value)
		if value == "" {
			value = "other"
		}
		values[key] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]attribute.KeyValue, 0, len(keys))
	for _, key := range keys {
		result = append(result, attribute.String(key, values[key]))
	}
	return result
}

func normalizeMetricLabelValue(key, value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || len(value) > maxMetricLabelValueLength || metricUUIDPattern.MatchString(value) || metricNumericPathID.MatchString(value) || metricULIDPattern.MatchString(value) || isNumericID(value) || !metricLabelValuePattern.MatchString(value) {
		return ""
	}
	if allowed, ok := controlledMetricValues[key]; ok {
		if _, ok := allowed[value]; !ok {
			return ""
		}
	}
	return value
}

func isNumericID(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// RuntimeMetrics exposes counters that can be used by exporters, collectors,
// and instrumentation adapters to report their own failure without changing
// business control flow.
type RuntimeMetrics struct {
	ExportFailures otelmetric.Int64Counter
	Dropped        otelmetric.Int64Counter
}

// NewRuntimeMetrics creates the shared telemetry-health instruments on the
// supplied meter. Passing a nil meter is a programmer error and is returned to
// the caller before any business work starts.
func NewRuntimeMetrics(meter otelmetric.Meter) (RuntimeMetrics, error) {
	if meter == nil {
		return RuntimeMetrics{}, errors.New("observability meter is nil")
	}
	exportFailures, err := meter.Int64Counter(
		"oj.telemetry.export.failure",
		otelmetric.WithDescription("Metric and trace export failures"),
		otelmetric.WithUnit("1"),
	)
	if err != nil {
		return RuntimeMetrics{}, err
	}
	dropped, err := meter.Int64Counter(
		"oj.telemetry.dropped",
		otelmetric.WithDescription("Telemetry measurements dropped before export"),
		otelmetric.WithUnit("1"),
	)
	if err != nil {
		return RuntimeMetrics{}, err
	}
	return RuntimeMetrics{ExportFailures: exportFailures, Dropped: dropped}, nil
}

func (m RuntimeMetrics) RecordExportFailure(ctx context.Context, code string) {
	if m.ExportFailures == nil {
		return
	}
	m.ExportFailures.Add(ctx, 1, otelmetric.WithAttributes(Attributes(Label{Key: LabelErrorCode, Value: code})...))
}

func (m RuntimeMetrics) RecordDropped(ctx context.Context, code string) {
	if m.Dropped == nil {
		return
	}
	m.Dropped.Add(ctx, 1, otelmetric.WithAttributes(Attributes(Label{Key: LabelErrorCode, Value: code})...))
}

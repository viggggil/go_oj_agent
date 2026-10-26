package observability

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestAttributesAllowlistAndNormalizeValues(t *testing.T) {
	got := Attributes(
		Label{Key: LabelStatus, Value: " COMPLETED "},
		Label{Key: LabelRoute, Value: "/api/v1/submissions/{id}"},
		Label{Key: LabelRPC, Value: "/api/v1/submissions/7"},
		Label{Key: "submission_id", Value: "42"},
		Label{Key: LabelTool, Value: "550e8400-e29b-41d4-a716-446655440000"},
		Label{Key: LabelModel, Value: "7"},
		Label{Key: LabelPurpose, Value: "0190f7b2-7b3e-7c4b-8d59-6a8e6f2b9d10"},
		Label{Key: LabelErrorCode, Value: "AGENT_MODEL_TIMEOUT"},
	)
	if len(got) != 7 {
		t.Fatalf("attributes = %#v, want seven allowlisted keys", got)
	}
	if got[0].Key != LabelErrorCode || got[0].Value.AsString() != "agent_model_timeout" {
		t.Fatalf("attributes are not sorted/normalized: %#v", got)
	}
	for _, item := range got {
		if item.Key == "submission_id" || item.Value.AsString() == "550e8400-e29b-41d4-a716-446655440000" || item.Value.AsString() == "42" {
			t.Fatalf("sensitive identifier reached metric attributes: %#v", got)
		}
	}
}

func TestRuntimeMetricsRecordWithManualReader(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	runtime, err := NewRuntimeMetrics(provider.Meter(InstrumentationName))
	if err != nil {
		t.Fatal(err)
	}
	runtime.RecordExportFailure(context.Background(), "OTEL_EXPORT_FAILED")
	runtime.RecordDropped(context.Background(), "queue_full")

	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatal(err)
	}
	if len(data.ScopeMetrics) != 1 || len(data.ScopeMetrics[0].Metrics) != 2 {
		t.Fatalf("metrics = %#v, want two runtime metrics", data.ScopeMetrics)
	}
	provider.Shutdown(context.Background())
}

func TestAttributesCollapseDynamicValues(t *testing.T) {
	got := Attributes(
		Label{Key: LabelRoute, Value: "/api/v1/users/alice"},
		Label{Key: LabelModel, Value: "tenant-acme"},
		Label{Key: LabelTool, Value: "01HZX5YJ4VEXAMPLE0000000000"},
		Label{Key: LabelErrorCode, Value: "PROVIDER_SECRET_123"},
	)
	for _, item := range got {
		if item.Value.AsString() != "other" {
			t.Fatalf("dynamic metric value escaped: %#v", got)
		}
	}
}

func TestNewRuntimeMetricsRejectsNilMeter(t *testing.T) {
	if _, err := NewRuntimeMetrics(nil); err == nil {
		t.Fatal("nil meter accepted")
	}
}

func TestJudgeMetricsRecordBoundedSeries(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	metrics := NewJudgeMetrics(provider.Meter(InstrumentationName))
	ctx := context.Background()
	metrics.Submission(ctx, "go", "success")
	metrics.WorkerStart(ctx)
	metrics.WorkerTask(ctx, "go", "success")
	metrics.WorkerRetry(ctx, "go")
	metrics.WorkerDuration(ctx, 12*time.Millisecond, "go", "success")
	metrics.Queue(ctx, time.Second)
	metrics.Compile(ctx, time.Millisecond, "success")
	metrics.Execute(ctx, 2*time.Millisecond, "go", "WA")
	metrics.Sandbox(ctx, time.Millisecond, "go", "compile")
	metrics.OutboxPublished(ctx, "success")
	metrics.Result(ctx, "WA", "success")
	metrics.ResultPublishFailed(ctx)
	metrics.WorkerStop(ctx)

	var data metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &data); err != nil {
		t.Fatal(err)
	}
	if len(data.ScopeMetrics) != 1 {
		t.Fatalf("scope metrics = %d, want 1", len(data.ScopeMetrics))
	}
	seen := make(map[string]bool)
	for _, item := range data.ScopeMetrics[0].Metrics {
		seen[item.Name] = true
	}
	for _, name := range []string{
		"oj.judge.submission.total", "oj.judge.result.total", "oj.judge.result.publish.failure.total", "oj.judge.queue.duration",
		"oj.judge.compile.duration", "oj.judge.execute.duration", "oj.judge.outbox.publish.total",
		"oj.judge.worker.task.total", "oj.judge.worker.retry.total", "oj.judge.worker.in_flight", "oj.judge.worker.task.duration",
		"oj.judge.worker.sandbox.duration",
	} {
		if !seen[name] {
			t.Errorf("metric %q was not collected", name)
		}
	}
	provider.Shutdown(ctx)
}

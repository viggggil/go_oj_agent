package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestNewJSONLoggerAddsServiceAndTraceFields(t *testing.T) {
	var output bytes.Buffer
	logger := NewJSONLogger(&output, "judge-worker")
	traceID, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	spanID, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled}))
	logger.InfoContext(ctx, "task finished", "error_code", "JUDGE_FAILED")

	var entry map[string]any
	if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry["service_name"] != "judge-worker" || entry["message"] != "task finished" || entry["error_code"] != "JUDGE_FAILED" {
		t.Fatalf("entry = %#v", entry)
	}
	if entry["trace_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" || entry["span_id"] != "00f067aa0ba902b7" {
		t.Fatalf("trace fields = %#v", entry)
	}
}

func TestContextHandlerPreservesWithAttrs(t *testing.T) {
	var output bytes.Buffer
	logger := NewJSONLogger(&output, "contest-service").With("component", "relay")
	logger.Log(context.Background(), slog.LevelWarn, "retry")
	var entry map[string]any
	if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry["component"] != "relay" || entry["service_name"] != "contest-service" {
		t.Fatalf("entry = %#v", entry)
	}
}

func TestNewJSONLoggerRedactsSensitiveFields(t *testing.T) {
	var output bytes.Buffer
	NewJSONLogger(&output, "judge-worker").Error("compile failed", "error_code", "JUDGE_COMPILE_FAILED", "stderr", "source line with private data", "error", "mysql://private")
	if strings.Contains(output.String(), "source line with private data") || strings.Contains(output.String(), "mysql://private") {
		t.Fatalf("sensitive log content escaped: %s", output.String())
	}
	if !strings.Contains(output.String(), "JUDGE_COMPILE_FAILED") {
		t.Fatalf("stable error code missing: %s", output.String())
	}
	if strings.Contains(output.String(), "mysql://private") {
		t.Fatalf("raw error attribute escaped: %s", output.String())
	}
}

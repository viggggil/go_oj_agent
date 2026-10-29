package observability

import (
	"context"
	"io"
	"log/slog"
	"strings"

	"go.opentelemetry.io/otel/trace"
)

// NewJSONLogger returns a structured logger with stable service and trace
// correlation fields. Trace fields are derived only from the current OTel
// context; callers cannot select a trace by passing an arbitrary string.
func NewJSONLogger(output io.Writer, service string) *slog.Logger {
	return slog.New(&contextHandler{
		Handler: slog.NewJSONHandler(output, &slog.HandlerOptions{ReplaceAttr: redactLogAttribute}),
		service: service,
	})
}

type contextHandler struct {
	slog.Handler
	service string
}

func (h *contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := *h
	clone.Handler = h.Handler.WithAttrs(attrs)
	return &clone
}

func (h *contextHandler) WithGroup(name string) slog.Handler {
	clone := *h
	clone.Handler = h.Handler.WithGroup(name)
	return &clone
}

func (h *contextHandler) Handle(ctx context.Context, record slog.Record) error {
	if h.service != "" {
		record.AddAttrs(slog.String("service_name", h.service))
	}
	spanContext := trace.SpanContextFromContext(ctx)
	if spanContext.IsValid() {
		record.AddAttrs(
			slog.String("trace_id", spanContext.TraceID().String()),
			slog.String("span_id", spanContext.SpanID().String()),
		)
	}
	return h.Handler.Handle(ctx, record)
}

func redactLogAttribute(_ []string, attr slog.Attr) slog.Attr {
	key := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(attr.Key, "-", "_"), ".", "_"))
	switch key {
	case "error", "err", "stderr", "stdout", "source", "source_code", "code", "prompt", "response", "token", "authorization", "cookie", "password", "dsn", "secret", "body", "input", "output", "tool_args", "tool_result", "rag_context":
		return slog.Attr{}
	case "time":
		attr.Key = "timestamp"
	case "msg":
		attr.Key = "message"
	case "error_code":
		if attr.Value.Kind() != slog.KindString || !stableLogCode(attr.Value.String()) {
			return slog.Attr{}
		}
	default:
	}
	return attr
}

func stableLogCode(value string) bool {
	if value == "" || value[0] < 'A' || value[0] > 'Z' || len(value) > 64 {
		return false
	}
	for _, char := range value {
		if (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '_' {
			return false
		}
	}
	return true
}

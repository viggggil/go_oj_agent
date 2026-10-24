package mq

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const (
	TraceParentHeader = "traceparent"
	TraceStateHeader  = "tracestate"
)

// TraceHeadersFromContext returns W3C trace context suitable for AMQP headers.
// Invalid or absent contexts intentionally produce an empty table.
func TraceHeadersFromContext(ctx context.Context) amqp091.Table {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	headers := amqp091.Table{}
	for key, value := range carrier {
		if strings.TrimSpace(value) != "" {
			headers[key] = value
		}
	}
	return headers
}

// ContextFromTraceHeaders extracts W3C trace context from AMQP headers. A
// malformed or missing header starts a fresh trace and never rejects a message.
func ContextFromTraceHeaders(ctx context.Context, headers amqp091.Table) context.Context {
	carrier := propagation.MapCarrier{}
	for _, key := range []string{TraceParentHeader, TraceStateHeader} {
		if value, ok := headers[key].(string); ok {
			carrier[key] = value
		}
	}
	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}

// TraceContextValues returns the persisted W3C values used by an Outbox row.
func TraceContextValues(ctx context.Context) (traceparent, tracestate string) {
	headers := TraceHeadersFromContext(ctx)
	traceparent, _ = headers[TraceParentHeader].(string)
	tracestate, _ = headers[TraceStateHeader].(string)
	return traceparent, tracestate
}

func TraceHeadersFromValues(traceparent, tracestate string) amqp091.Table {
	headers := amqp091.Table{}
	if strings.TrimSpace(traceparent) != "" {
		headers[TraceParentHeader] = traceparent
	}
	if strings.TrimSpace(tracestate) != "" {
		headers[TraceStateHeader] = tracestate
	}
	return headers
}

func ContextFromTraceValues(ctx context.Context, traceparent, tracestate string) context.Context {
	return ContextFromTraceHeaders(ctx, TraceHeadersFromValues(traceparent, tracestate))
}

// ContextFromMessage prefers AMQP W3C headers and falls back to optional W3C
// fields in the stable envelope for mixed-version producers.
func ContextFromMessage(ctx context.Context, headers amqp091.Table, body []byte) context.Context {
	extracted := ContextFromTraceHeaders(ctx, headers)
	if trace.SpanContextFromContext(extracted).IsValid() {
		return extracted
	}
	var envelope Envelope
	if json.Unmarshal(body, &envelope) != nil {
		return extracted
	}
	return ContextFromTraceValues(ctx, envelope.TraceParent, envelope.TraceState)
}

package mq

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Envelope struct {
	EventID      string          `json:"event_id"`
	EventType    string          `json:"event_type"`
	EventVersion int32           `json:"event_version"`
	OccurredAt   time.Time       `json:"occurred_at"`
	TraceID      string          `json:"trace_id,omitempty"`
	TraceParent  string          `json:"traceparent,omitempty"`
	TraceState   string          `json:"tracestate,omitempty"`
	CausationID  string          `json:"causation_id,omitempty"`
	Data         json.RawMessage `json:"data"`
}

type EnvelopeMetadata struct {
	EventID      string
	EventType    string
	EventVersion int32
	OccurredAt   time.Time
	TraceID      string
	TraceParent  string
	TraceState   string
	CausationID  string
}

func MarshalEnvelope(metadata EnvelopeMetadata, data any) ([]byte, error) {
	if err := validateMetadata(metadata); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("marshal event data: %w", err)
	}
	envelope := Envelope{
		EventID: metadata.EventID, EventType: metadata.EventType, EventVersion: metadata.EventVersion,
		OccurredAt: metadata.OccurredAt.UTC(), TraceID: metadata.TraceID, TraceParent: metadata.TraceParent, TraceState: metadata.TraceState, CausationID: metadata.CausationID, Data: payload,
	}
	return json.Marshal(envelope)
}

func UnmarshalEnvelope(body []byte, expectedType string, data any) (Envelope, error) {
	var envelope Envelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return Envelope{}, fmt.Errorf("unmarshal event envelope: %w", err)
	}
	if err := validateMetadata(EnvelopeMetadata{
		EventID: envelope.EventID, EventType: envelope.EventType, EventVersion: envelope.EventVersion,
		OccurredAt: envelope.OccurredAt, TraceID: envelope.TraceID, TraceParent: envelope.TraceParent, TraceState: envelope.TraceState, CausationID: envelope.CausationID,
	}); err != nil {
		return Envelope{}, err
	}
	if expectedType != "" && envelope.EventType != expectedType {
		return Envelope{}, fmt.Errorf("event type %q does not match %q", envelope.EventType, expectedType)
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return Envelope{}, fmt.Errorf("event data is required")
	}
	if err := json.Unmarshal(envelope.Data, data); err != nil {
		return Envelope{}, fmt.Errorf("unmarshal event data: %w", err)
	}
	return envelope, nil
}

// WithTraceContext replaces only tracing metadata in an existing envelope.
// Payload bytes and event identity remain unchanged.
func WithTraceContext(body []byte, traceparent, tracestate string) ([]byte, error) {
	var envelope Envelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("unmarshal event envelope: %w", err)
	}
	envelope.TraceParent = traceparent
	envelope.TraceState = tracestate
	return json.Marshal(envelope)
}

func validateMetadata(metadata EnvelopeMetadata) error {
	if _, err := uuid.Parse(metadata.EventID); err != nil {
		return fmt.Errorf("invalid event id")
	}
	if metadata.EventType == "" || metadata.EventVersion != EventVersion1 || metadata.OccurredAt.IsZero() {
		return fmt.Errorf("invalid event metadata")
	}
	if metadata.CausationID != "" {
		if _, err := uuid.Parse(metadata.CausationID); err != nil {
			return fmt.Errorf("invalid causation id")
		}
	}
	if len(metadata.TraceParent) > 255 || len(metadata.TraceState) > 512 {
		return fmt.Errorf("invalid trace context")
	}
	return nil
}

package mq

import (
	"encoding/json"
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestSubmissionJudgedContract(t *testing.T) {
	now := time.Now().UTC()
	event := SubmissionJudged{SubmissionID: 1, ContestID: 2, UserID: 3, ProblemID: 4, Verdict: "AC", SubmittedAt: now, JudgedAt: now.Add(time.Second)}
	body, err := MarshalEnvelope(EnvelopeMetadata{EventID: uuid.NewString(), EventType: EventSubmissionJudged, EventVersion: EventVersion1, OccurredAt: now}, event)
	if err != nil {
		t.Fatal(err)
	}
	var decoded SubmissionJudged
	envelope, err := UnmarshalEnvelope(body, EventSubmissionJudged, &decoded)
	if err != nil || decoded.Validate() != nil || !decoded.SubmittedAt.Equal(event.SubmittedAt) || decoded.ContestID != 2 {
		t.Fatalf("decoded=%+v err=%v", decoded, err)
	}
	envelope.EventVersion = 2
	invalid, _ := json.Marshal(envelope)
	if _, err := UnmarshalEnvelope(invalid, EventSubmissionJudged, &decoded); err == nil {
		t.Fatal("accepted unknown version")
	}
	if _, err := UnmarshalEnvelope(body, EventSubmissionInvalidated, &decoded); err == nil {
		t.Fatal("accepted wrong type")
	}
	for _, mutate := range []func(*SubmissionJudged){func(e *SubmissionJudged) { e.ContestID = 0 }, func(e *SubmissionJudged) { e.SubmissionID = 0 }, func(e *SubmissionJudged) { e.UserID = 0 }, func(e *SubmissionJudged) { e.ProblemID = 0 }, func(e *SubmissionJudged) { e.SubmittedAt = time.Time{} }, func(e *SubmissionJudged) { e.JudgedAt = time.Time{} }, func(e *SubmissionJudged) { e.Verdict = "UNKNOWN" }} {
		value := event
		mutate(&value)
		if value.Validate() == nil {
			t.Fatalf("accepted %+v", value)
		}
	}
	event.Verdict = "SYSTEM_ERROR"
	if err := event.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestSubmissionInvalidatedContract(t *testing.T) {
	now := time.Now().UTC()
	event := SubmissionInvalidated{SubmissionID: 1, ContestID: 2, UserID: 3, ProblemID: 4, SubmittedAt: now, InvalidatedAt: now.Add(time.Second)}
	body, err := MarshalEnvelope(EnvelopeMetadata{EventID: uuid.NewString(), EventType: EventSubmissionInvalidated, EventVersion: EventVersion1, OccurredAt: now}, event)
	if err != nil {
		t.Fatal(err)
	}
	var decoded SubmissionInvalidated
	if _, err := UnmarshalEnvelope(body, EventSubmissionInvalidated, &decoded); err != nil || decoded.Validate() != nil {
		t.Fatalf("decoded=%+v err=%v", decoded, err)
	}
	decoded.SubmittedAt = time.Time{}
	if decoded.Validate() == nil {
		t.Fatal("accepted missing contest submitted_at")
	}
	decoded.ContestID = 0
	if err := decoded.Validate(); err != nil {
		t.Fatal("legacy ordinary invalidation rejected", err)
	}
}

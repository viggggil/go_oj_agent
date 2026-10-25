package biz

import (
	"testing"
	"time"

	"github.com/viggggil/go_oj_agent/pkg/mq"
)

func TestParseProjectionEventCarriesTraceContext(t *testing.T) {
	traceparent := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	body, err := mq.MarshalEnvelope(mq.EnvelopeMetadata{
		EventID: "123e4567-e89b-12d3-a456-426614174099", EventType: mq.EventSubmissionJudged,
		EventVersion: mq.EventVersion1, OccurredAt: time.Now().UTC(), TraceParent: traceparent, CausationID: "123e4567-e89b-12d3-a456-426614174098",
	}, mq.SubmissionJudged{SubmissionID: 9, ContestID: 7, UserID: 42, ProblemID: 3, Verdict: "AC", SubmittedAt: time.Now().UTC(), JudgedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	event, err := ParseProjectionEvent(body)
	if err != nil {
		t.Fatal(err)
	}
	if event.TraceParent != traceparent || event.TraceState != "" || event.CausationID == "" || event.Fact.SubmissionID != 9 {
		t.Fatalf("projection event=%+v", event)
	}
}

func TestACMScoring(t *testing.T) {
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	fact := func(id int64, v string, minute int) SubmissionFact {
		return SubmissionFact{SubmissionJudged: mq.SubmissionJudged{SubmissionID: id, ProblemID: 7, Verdict: v, SubmittedAt: start.Add(time.Duration(minute) * time.Minute)}}
	}
	invalid := fact(2, "AC", 10)
	invalid.Invalidated = true
	tests := []struct {
		name     string
		facts    []SubmissionFact
		solved   bool
		wrong    int32
		penalty  int64
		accepted int64
	}{
		{"WA then AC", []SubmissionFact{fact(1, "WA", 5), fact(2, "AC", 10)}, true, 1, 1800, 2},
		{"two errors", []SubmissionFact{fact(1, "WA", 2), fact(2, "WA", 5), fact(3, "AC", 10)}, true, 2, 3000, 3},
		{"WA after AC", []SubmissionFact{fact(1, "AC", 5), fact(2, "WA", 10)}, true, 0, 300, 1},
		{"system failure", []SubmissionFact{fact(1, "SYSTEM_ERROR", 5), fact(2, "AC", 10)}, true, 0, 600, 2},
		{"out of order", []SubmissionFact{fact(2, "AC", 10), fact(1, "WA", 5)}, true, 1, 1800, 2},
		{"invalidated AC", []SubmissionFact{fact(1, "WA", 5), invalid}, false, 1, 0, 0},
		{"updated AC to WA", []SubmissionFact{fact(1, "WA", 5), fact(2, "WA", 10)}, false, 2, 0, 0},
		{"tie uses ID", []SubmissionFact{fact(2, "AC", 5), fact(1, "CE", 5)}, true, 1, 1500, 2},
		{"all wrong verdicts", []SubmissionFact{fact(1, "WA", 1), fact(2, "TLE", 2), fact(3, "MLE", 3), fact(4, "RE", 4), fact(5, "CE", 5)}, false, 5, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := (ACMScoringPolicy{}).Rebuild(start, tt.facts)
			if result.Solved != tt.solved || result.WrongAttempts != tt.wrong || result.PenaltySeconds != tt.penalty {
				t.Fatalf("result=%+v", result)
			}
			if tt.accepted != 0 && (result.AcceptedSubmissionID == nil || *result.AcceptedSubmissionID != tt.accepted || !result.AcceptedAt.Equal(start.Add(time.Duration(tt.penalty-int64(tt.wrong)*1200)*time.Second))) {
				t.Fatalf("accepted=%+v", result)
			}
		})
	}
}

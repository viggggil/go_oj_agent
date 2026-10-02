package biz

import (
	"github.com/viggggil/go_oj_agent/pkg/mq"
	"testing"
	"time"
)

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

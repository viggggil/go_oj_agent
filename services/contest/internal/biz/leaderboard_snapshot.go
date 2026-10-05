package biz

import (
	"fmt"
	"strconv"
	"time"
)

// Snapshot contains a complete, immutable user result. Version is a decimal
// string so JSON and Lua cannot silently round integers above 2^53.
type LeaderboardSnapshot struct {
	Schema         int                  `json:"schema"`
	ContestID      int64                `json:"contest_id"`
	UserID         int64                `json:"user_id"`
	Version        string               `json:"version"`
	SolvedCount    int32                `json:"solved_count"`
	PenaltySeconds int64                `json:"penalty_seconds"`
	Problems       []LeaderboardProblem `json:"problems"`
}
type LeaderboardProblem struct {
	ProblemID      int64      `json:"problem_id"`
	Solved         bool       `json:"solved"`
	WrongAttempts  int32      `json:"wrong_attempts"`
	AcceptedAt     *time.Time `json:"accepted_at,omitempty"`
	PenaltySeconds int64      `json:"penalty_seconds"`
}

func (s LeaderboardSnapshot) Validate() error {
	version, err := strconv.ParseInt(s.Version, 10, 64)
	if err != nil || version <= 0 || strconv.FormatInt(version, 10) != s.Version || s.Schema != 1 || s.ContestID <= 0 || s.UserID <= 0 || len(s.Problems) == 0 || len(s.Problems) > 100 || s.SolvedCount < 0 || s.SolvedCount > 100 || s.PenaltySeconds < 0 {
		return fmt.Errorf("invalid leaderboard snapshot identity or totals")
	}
	seen := make(map[int64]bool, len(s.Problems))
	var solved int32
	var penalty int64
	for _, p := range s.Problems {
		if p.ProblemID <= 0 || seen[p.ProblemID] || p.WrongAttempts < 0 || p.PenaltySeconds < 0 || p.PenaltySeconds > int64(^uint64(0)>>1)-penalty || (p.Solved && (p.AcceptedAt == nil || p.AcceptedAt.IsZero())) || (!p.Solved && (p.AcceptedAt != nil || p.PenaltySeconds != 0)) {
			return fmt.Errorf("invalid leaderboard problem result")
		}
		seen[p.ProblemID] = true
		if p.Solved {
			solved++
		}
		penalty += p.PenaltySeconds
	}
	if solved != s.SolvedCount || penalty != s.PenaltySeconds {
		return fmt.Errorf("snapshot totals disagree with problem results")
	}
	return nil
}

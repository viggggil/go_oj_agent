package data

import (
	"testing"
	"time"

	"github.com/viggggil/go_oj_agent/services/contest/internal/biz"
)

func BenchmarkContestSignatureAndMember(b *testing.B) {
	contest := biz.Contest{ID: 20, StartAt: time.Unix(1_700_000_000, 0).UTC(), EndAt: time.Unix(1_700_003_600, 0).UTC()}
	for i := int64(1); i <= 100; i++ {
		contest.Problems = append(contest.Problems, biz.ContestProblem{ProblemID: i, SortOrder: int32(i), Score: 100})
	}
	snapshot := testSnapshot(42, "1", 10, 1234)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = contestSignature(contest)
		_ = leaderboardMember(snapshot)
	}
}

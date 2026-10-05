package data

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"github.com/viggggil/go_oj_agent/services/contest/internal/biz"
)

// StreamLeaderboard holds one consistent read view but buffers at most 100
// users (each with at most 100 problems). It never uses an Outbox ID watermark:
// auto-increment allocation does not imply transaction commit order.
func (r *Repository) StreamLeaderboard(ctx context.Context, id int64, emit func(biz.LeaderboardSnapshot) error) (biz.Contest, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return biz.Contest{}, err
	}
	defer tx.Rollback()
	contest := biz.Contest{ID: id}
	if err := tx.QueryRowContext(ctx, `SELECT start_at,end_at FROM contests WHERE id=?`, id).Scan(&contest.StartAt, &contest.EndAt); err != nil {
		return contest, err
	}
	if err := scanProblems(ctx, tx, &contest); err != nil {
		return contest, err
	}
	var summarized, projected int64
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM contest_user_results WHERE contest_id=?`, id).Scan(&summarized); err != nil {
		return contest, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(DISTINCT user_id) FROM contest_problem_results WHERE contest_id=?`, id).Scan(&projected); err != nil {
		return contest, err
	}
	if summarized != projected {
		return contest, fmt.Errorf("leaderboard summaries missing; run migration backfill")
	}
	var lastUser int64
	for {
		rows, err := tx.QueryContext(ctx, `SELECT user_id,solved_count,penalty_seconds,version FROM contest_user_results WHERE contest_id=? AND user_id>? ORDER BY user_id LIMIT 100`, id, lastUser)
		if err != nil {
			return contest, err
		}
		batch := make([]biz.LeaderboardSnapshot, 0, 100)
		positions := make(map[int64]int, 100)
		for rows.Next() {
			s := biz.LeaderboardSnapshot{Schema: 1, ContestID: id}
			var version int64
			if err := rows.Scan(&s.UserID, &s.SolvedCount, &s.PenaltySeconds, &version); err != nil {
				rows.Close()
				return contest, err
			}
			s.Version = strconv.FormatInt(version, 10)
			positions[s.UserID] = len(batch)
			batch = append(batch, s)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return contest, err
		}
		if len(batch) == 0 {
			break
		}
		upper := batch[len(batch)-1].UserID
		rows, err = tx.QueryContext(ctx, `SELECT u.user_id,p.problem_id,COALESCE(r.solved,0),COALESCE(r.wrong_attempts,0),r.accepted_at,COALESCE(r.penalty_seconds,0)
 FROM contest_user_results u JOIN contest_problems p ON p.contest_id=u.contest_id
 LEFT JOIN contest_problem_results r ON r.contest_id=u.contest_id AND r.user_id=u.user_id AND r.problem_id=p.problem_id
 WHERE u.contest_id=? AND u.user_id>? AND u.user_id<=? ORDER BY u.user_id,p.sort_order,p.problem_id`, id, lastUser, upper)
		if err != nil {
			return contest, err
		}
		for rows.Next() {
			var user int64
			var p biz.LeaderboardProblem
			var accepted sql.NullTime
			if err := rows.Scan(&user, &p.ProblemID, &p.Solved, &p.WrongAttempts, &accepted, &p.PenaltySeconds); err != nil {
				rows.Close()
				return contest, err
			}
			if accepted.Valid {
				at := accepted.Time.UTC()
				p.AcceptedAt = &at
			}
			i, ok := positions[user]
			if !ok {
				rows.Close()
				return contest, fmt.Errorf("inconsistent leaderboard batch")
			}
			batch[i].Problems = append(batch[i].Problems, p)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return contest, err
		}
		for _, s := range batch {
			if err := s.Validate(); err != nil {
				return contest, err
			}
			if err := emit(s); err != nil {
				return contest, err
			}
		}
		lastUser = upper
	}
	return contest, tx.Commit()
}

func (r *Repository) RebuildLeaderboard(ctx context.Context, id int64) error {
	if r.leaderboard == nil {
		return ErrCacheIncomplete
	}
	leaderboardCacheRebuilds.Add(1)
	cache := r.leaderboard.cache
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	// Registration is BEFORE the first consistent MySQL read. Any ACK before
	// registration is already committed and included; ACKs afterwards have to
	// write the build target. Delayed snapshot rows cannot overwrite newer versions.
	generation, err := cache.BeginBuild(ctx, id)
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), cache.timeout)
		defer cancel()
		_ = cache.AbandonBuild(cleanup, id, generation)
	}()
	renewCtx, stopRenew := context.WithCancel(ctx)
	renewDone := make(chan struct{})
	go func() {
		defer close(renewDone)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-renewCtx.Done():
				return
			case <-ticker.C:
				if err := cache.RenewBuild(renewCtx, id, generation); err != nil {
					if renewCtx.Err() == nil {
						cancel()
					}
					return
				}
			}
		}
	}()
	contest, err := r.StreamLeaderboard(ctx, id, func(s biz.LeaderboardSnapshot) error { return cache.Load(ctx, generation, s) })
	stopRenew()
	<-renewDone
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := cache.ActivateBuild(ctx, id, generation, contestSignature(contest)); err != nil {
		return err
	}
	// A switched generation cannot serve reads until DB lag is verified. A crash
	// between activation and verification simply causes MySQL fallback.
	return r.verifyLeaderboard(ctx, contest, generation)
}

func (r *Repository) verifyLeaderboard(ctx context.Context, contest biz.Contest, generation string) error {
	start := time.Now()
	var lagMicros int64
	// DB time computes queue age; Redis time computes verification age. Adding
	// call duration is conservative and avoids relying on synchronized host clocks.
	if err := r.db.QueryRowContext(ctx, `SELECT COALESCE(GREATEST(TIMESTAMPDIFF(MICROSECOND,MIN(created_at),UTC_TIMESTAMP(6)),0),0) FROM contest_cache_outbox WHERE contest_id=? AND status IN ('pending','dead')`, contest.ID).Scan(&lagMicros); err != nil {
		return err
	}
	lag := maxCacheAge + time.Second
	if lagMicros < lag.Microseconds() {
		lag = time.Duration(lagMicros)*time.Microsecond + time.Since(start) + r.leaderboard.cache.timeout
	}
	return r.leaderboard.cache.VerifyFreshness(ctx, contest.ID, generation, contestSignature(contest), lag)
}

package data

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/viggggil/go_oj_agent/services/contest/internal/biz"
	"strconv"
)

// Caller holds the participant row lock; totals, version and immutable outbox
// payload are committed in the same transaction as the result facts.
func enqueueLeaderboard(ctx context.Context, tx *sql.Tx, contestID, userID int64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO contest_user_results (contest_id,user_id,solved_count,penalty_seconds,version,updated_at)
 SELECT contest_id,user_id,SUM(solved),SUM(penalty_seconds),1,UTC_TIMESTAMP(6) FROM contest_problem_results WHERE contest_id=? AND user_id=? GROUP BY contest_id,user_id
 ON DUPLICATE KEY UPDATE solved_count=VALUES(solved_count),penalty_seconds=VALUES(penalty_seconds),version=version+1,updated_at=UTC_TIMESTAMP(6)`, contestID, userID)
	if err != nil {
		return err
	}
	return enqueueCurrentSummary(ctx, tx, contestID, userID)
}
func enqueueCurrentSummary(ctx context.Context, tx *sql.Tx, contestID, userID int64) error {
	s := biz.LeaderboardSnapshot{Schema: 1, ContestID: contestID, UserID: userID}
	var version int64
	if err := tx.QueryRowContext(ctx, `SELECT solved_count,penalty_seconds,version FROM contest_user_results WHERE contest_id=? AND user_id=?`, contestID, userID).Scan(&s.SolvedCount, &s.PenaltySeconds, &version); err != nil {
		return err
	}
	s.Version = strconv.FormatInt(version, 10)
	rows, err := tx.QueryContext(ctx, `SELECT p.problem_id,COALESCE(r.solved,0),COALESCE(r.wrong_attempts,0),r.accepted_at,COALESCE(r.penalty_seconds,0)
 FROM contest_problems p LEFT JOIN contest_problem_results r ON r.contest_id=p.contest_id AND r.problem_id=p.problem_id AND r.user_id=?
 WHERE p.contest_id=? ORDER BY p.sort_order,p.problem_id`, userID, contestID)
	if err != nil {
		return err
	}
	s.Problems = make([]biz.LeaderboardProblem, 0)
	for rows.Next() {
		var p biz.LeaderboardProblem
		var accepted sql.NullTime
		if err := rows.Scan(&p.ProblemID, &p.Solved, &p.WrongAttempts, &accepted, &p.PenaltySeconds); err != nil {
			rows.Close()
			return err
		}
		if accepted.Valid {
			at := accepted.Time.UTC()
			p.AcceptedAt = &at
		}
		s.Problems = append(s.Problems, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if err := s.Validate(); err != nil {
		return err
	}
	payload, err := json.Marshal(s)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO contest_cache_outbox (event_id,contest_id,user_id,result_version,payload,next_retry_at,created_at) VALUES (?,?,?,?,?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`, uuid.NewString(), contestID, userID, version, payload)
	return err
}

// BackfillLeaderboard requires result consumers to be paused for the migration.
// Reruns skip already initialized users. Each new user and its version-1 event
// are committed atomically, with bounded transactions and resumable progress.
func (r *Repository) BackfillLeaderboard(ctx context.Context) (int, error) {
	var lastContest, lastUser int64
	count := 0
	for {
		var contestID, userID int64
		err := r.db.QueryRowContext(ctx, `SELECT contest_id,user_id FROM contest_problem_results WHERE contest_id>? OR (contest_id=? AND user_id>?) GROUP BY contest_id,user_id ORDER BY contest_id,user_id LIMIT 1`, lastContest, lastContest, lastUser).Scan(&contestID, &userID)
		if err == sql.ErrNoRows {
			return count, nil
		}
		if err != nil {
			return count, err
		}
		changed, err := r.backfillUser(ctx, contestID, userID)
		if err != nil {
			return count, err
		}
		if changed {
			count++
		}
		lastContest, lastUser = contestID, userID
	}
}
func (r *Repository) backfillUser(ctx context.Context, contestID, userID int64) (bool, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var id int64
	if err := tx.QueryRowContext(ctx, `SELECT user_id FROM contest_participants WHERE contest_id=? AND user_id=? FOR UPDATE`, contestID, userID).Scan(&id); err != nil {
		return false, err
	}
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM contest_user_results WHERE contest_id=? AND user_id=?`, contestID, userID).Scan(&exists); err != nil {
		return false, err
	}
	if exists != 0 {
		return false, tx.Commit()
	}
	if err := enqueueLeaderboard(ctx, tx, contestID, userID); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

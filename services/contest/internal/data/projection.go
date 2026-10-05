package data

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	contestv1 "github.com/viggggil/go_oj_agent/api/contest/v1"
	"github.com/viggggil/go_oj_agent/services/contest/internal/biz"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (r *Repository) ApplyProjection(ctx context.Context, event biz.ProjectionEvent) error {
	return withContestTx(ctx, r.db, func(tx *sql.Tx) error {
		return r.applyProjectionTx(ctx, tx, event)
	})
}

func (r *Repository) applyProjectionTx(ctx context.Context, tx *sql.Tx, event biz.ProjectionEvent) error {
	// 共享比赛锁保护配置；不同用户之间共享锁兼容，不串行化整场结果。
	f := event.Fact
	var start, end time.Time
	if err := tx.QueryRowContext(ctx, `SELECT start_at,end_at FROM contests WHERE id=? FOR SHARE`, f.ContestID).Scan(&start, &end); err != nil {
		return err
	}
	inserted, err := tx.ExecContext(ctx, `INSERT IGNORE INTO contest_processed_events (consumer_name,event_id,processed_at) VALUES ('contest-result-consumer',?,UTC_TIMESTAMP(6))`, event.EventID)
	if err != nil {
		return err
	}
	n, err := inserted.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	// 与 DATETIME(6) 的版本精度一致，避免纳秒重放误判为新版本。
	f.SubmittedAt = f.SubmittedAt.UTC().Truncate(time.Microsecond)
	f.JudgedAt = f.JudgedAt.UTC().Truncate(time.Microsecond)
	var user int64
	// 同一参与者串行更新，避免并发首次插入和重算产生丢失更新。
	if err := tx.QueryRowContext(ctx, `SELECT user_id FROM contest_participants WHERE contest_id=? AND user_id=? FOR UPDATE`, f.ContestID, f.UserID).Scan(&user); err != nil {
		return err
	}
	var problem int64
	if err := tx.QueryRowContext(ctx, `SELECT problem_id FROM contest_problems WHERE contest_id=? AND problem_id=?`, f.ContestID, f.ProblemID).Scan(&problem); err != nil {
		return err
	}
	if f.SubmittedAt.Before(start) || !f.SubmittedAt.Before(end) {
		return fmt.Errorf("submission outside contest interval")
	}
	var oldContest, oldUser, oldProblem int64
	var oldSubmitted, oldJudged time.Time
	var oldInvalid bool
	err = tx.QueryRowContext(ctx, `SELECT contest_id,user_id,problem_id,submitted_at,judged_at,invalidated FROM contest_submission_results WHERE submission_id=? FOR UPDATE`, f.SubmissionID).Scan(&oldContest, &oldUser, &oldProblem, &oldSubmitted, &oldJudged, &oldInvalid)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		if oldContest != f.ContestID || oldUser != f.UserID || oldProblem != f.ProblemID || !oldSubmitted.Equal(f.SubmittedAt) {
			return fmt.Errorf("submission identity changed")
		}
		if oldInvalid || (!f.Invalidated && !f.JudgedAt.After(oldJudged)) {
			return nil
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO contest_submission_results (submission_id,contest_id,user_id,problem_id,verdict,submitted_at,judged_at,invalidated,updated_at) VALUES (?,?,?,?,?,?,?,?,UTC_TIMESTAMP(6)) ON DUPLICATE KEY UPDATE verdict=VALUES(verdict),judged_at=VALUES(judged_at),invalidated=VALUES(invalidated),updated_at=VALUES(updated_at)`, f.SubmissionID, f.ContestID, f.UserID, f.ProblemID, f.Verdict, f.SubmittedAt, f.JudgedAt, f.Invalidated)
	if err != nil {
		return err
	}
	// 不同参与者可能并发首次写入相同 ID；唯一键锁后再次核对身份，冲突事务必须回滚。
	var sameIdentity bool
	if err := tx.QueryRowContext(ctx, `SELECT contest_id=? AND user_id=? AND problem_id=? AND submitted_at=? FROM contest_submission_results WHERE submission_id=?`, f.ContestID, f.UserID, f.ProblemID, f.SubmittedAt, f.SubmissionID).Scan(&sameIdentity); err != nil {
		return err
	}
	if !sameIdentity {
		return fmt.Errorf("submission identity changed during insert")
	}
	rows, err := tx.QueryContext(ctx, `SELECT submission_id,verdict,submitted_at,invalidated FROM contest_submission_results WHERE contest_id=? AND user_id=? AND problem_id=? ORDER BY submitted_at ASC,submission_id ASC`, f.ContestID, f.UserID, f.ProblemID)
	if err != nil {
		return err
	}
	var facts []biz.SubmissionFact
	for rows.Next() {
		fact := biz.SubmissionFact{}
		fact.ProblemID = f.ProblemID
		if err := rows.Scan(&fact.SubmissionID, &fact.Verdict, &fact.SubmittedAt, &fact.Invalidated); err != nil {
			rows.Close()
			return err
		}
		facts = append(facts, fact)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	result := (biz.ACMScoringPolicy{}).Rebuild(start, facts)
	_, err = tx.ExecContext(ctx, `INSERT INTO contest_problem_results (contest_id,user_id,problem_id,solved,wrong_attempts,accepted_submission_id,accepted_at,penalty_seconds,updated_at) VALUES (?,?,?,?,?,?,?,?,UTC_TIMESTAMP(6)) ON DUPLICATE KEY UPDATE solved=VALUES(solved),wrong_attempts=VALUES(wrong_attempts),accepted_submission_id=VALUES(accepted_submission_id),accepted_at=VALUES(accepted_at),penalty_seconds=VALUES(penalty_seconds),updated_at=VALUES(updated_at)`, f.ContestID, f.UserID, f.ProblemID, result.Solved, result.WrongAttempts, result.AcceptedSubmissionID, result.AcceptedAt, result.PenaltySeconds)
	if err != nil {
		return err
	}
	if err := enqueueLeaderboard(ctx, tx, f.ContestID, f.UserID); err != nil {
		return err
	}
	return nil
}

func (r *Repository) Leaderboard(ctx context.Context, id int64, page, size int32) ([]*contestv1.LeaderboardEntry, int64, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	var total int64
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(DISTINCT user_id) FROM contest_problem_results WHERE contest_id=?`, id).Scan(&total); err != nil {
		return nil, 0, err
	}
	offset := (int64(page) - 1) * int64(size)
	rows, err := tx.QueryContext(ctx, `SELECT user_id,SUM(solved),SUM(penalty_seconds) FROM contest_problem_results WHERE contest_id=? GROUP BY user_id ORDER BY SUM(solved) DESC,SUM(penalty_seconds) ASC,user_id ASC LIMIT ? OFFSET ?`, id, size, offset)
	if err != nil {
		return nil, 0, err
	}
	items := make([]*contestv1.LeaderboardEntry, 0)
	for rows.Next() {
		item := &contestv1.LeaderboardEntry{Rank: int32(offset) + int32(len(items)) + 1}
		if err := rows.Scan(&item.UserId, &item.SolvedCount, &item.PenaltySeconds); err != nil {
			rows.Close()
			return nil, 0, err
		}
		item.AcceptedCount, item.Score, item.Penalty = item.SolvedCount, item.SolvedCount, item.PenaltySeconds
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	for _, item := range items {
		rows, err := tx.QueryContext(ctx, `SELECT p.problem_id,COALESCE(r.solved,0),COALESCE(r.wrong_attempts,0),r.accepted_at FROM contest_problems p LEFT JOIN contest_problem_results r ON r.contest_id=p.contest_id AND r.problem_id=p.problem_id AND r.user_id=? WHERE p.contest_id=? ORDER BY p.sort_order,p.problem_id`, item.UserId, id)
		if err != nil {
			return nil, 0, err
		}
		for rows.Next() {
			result := &contestv1.LeaderboardProblemResult{}
			var accepted sql.NullTime
			if err := rows.Scan(&result.ProblemId, &result.Solved, &result.WrongAttempts, &accepted); err != nil {
				rows.Close()
				return nil, 0, err
			}
			if accepted.Valid {
				result.AcceptedAt = timestamppb.New(accepted.Time)
			}
			item.Problems = append(item.Problems, result)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

package data

import (
	"context"
	"database/sql"
	"errors"
	"time"

	contestv1 "github.com/viggggil/go_oj_agent/api/contest/v1"
	"github.com/viggggil/go_oj_agent/services/contest/internal/biz"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (r *Repository) IsParticipant(ctx context.Context, contestID, userID int64) (bool, error) {
	if r == nil || r.db == nil {
		return false, status.Error(codes.Internal, "contest database is not configured")
	}
	var exists int
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM contest_participants WHERE contest_id=? AND user_id=?)`, contestID, userID).Scan(&exists)
	return exists == 1, err
}

func (r *Repository) HasProblem(ctx context.Context, contestID, problemID int64) (bool, error) {
	if r == nil || r.db == nil {
		return false, status.Error(codes.Internal, "contest database is not configured")
	}
	var exists int
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM contest_problems WHERE contest_id=? AND problem_id=?)`, contestID, problemID).Scan(&exists)
	return exists == 1, err
}

func (r *Repository) Join(ctx context.Context, contestID, userID int64) (time.Time, error) {
	if r == nil || r.db == nil {
		return time.Time{}, status.Error(codes.Internal, "contest database is not configured")
	}
	var joinedAt time.Time
	err := withContestTx(ctx, r.db, func(tx *sql.Tx) error {
		var startAt, endAt, dbNow time.Time
		var state string
		err := tx.QueryRowContext(ctx, `SELECT start_at,end_at,status,UTC_TIMESTAMP(3) FROM contests WHERE id=? FOR UPDATE`, contestID).Scan(&startAt, &endAt, &state, &dbNow)
		if errors.Is(err, sql.ErrNoRows) {
			return status.Error(codes.NotFound, "contest not found")
		}
		if err != nil {
			return err
		}
		if state == "archived" || !dbNow.Before(startAt) || !dbNow.Before(endAt) || state != "draft" {
			return status.Error(codes.FailedPrecondition, "contest is not accepting registrations")
		}
		if _, err := tx.ExecContext(ctx, `INSERT IGNORE INTO contest_participants (contest_id,user_id,joined_at) VALUES (?, ?, UTC_TIMESTAMP(3))`, contestID, userID); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, `SELECT joined_at FROM contest_participants WHERE contest_id=? AND user_id=?`, contestID, userID).Scan(&joinedAt)
	})
	if err != nil {
		return time.Time{}, err
	}
	return joinedAt.UTC(), nil
}

func (r *Repository) Create(ctx context.Context, contest biz.Contest) (biz.Contest, error) {
	if r == nil || r.db == nil {
		return biz.Contest{}, status.Error(codes.Internal, "contest database is not configured")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return biz.Contest{}, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	result, err := tx.ExecContext(ctx, `INSERT INTO contests (title,status,start_at,end_at,created_by,created_at,updated_at) VALUES (?, 'draft', ?, ?, ?, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3))`, contest.Title, contest.StartAt, contest.EndAt, contest.CreatedBy)
	if err != nil {
		return biz.Contest{}, err
	}
	contest.ID, err = result.LastInsertId()
	if err != nil {
		return biz.Contest{}, err
	}
	if err = replaceProblems(ctx, tx, contest.ID, contest.Problems); err != nil {
		return biz.Contest{}, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT created_at, updated_at FROM contests WHERE id = ?`, contest.ID).Scan(&contest.CreatedAt, &contest.UpdatedAt); err != nil {
		return biz.Contest{}, err
	}
	if err = tx.Commit(); err != nil {
		return biz.Contest{}, err
	}
	return contest, nil
}

func (r *Repository) Get(ctx context.Context, id int64) (biz.Contest, error) {
	if r == nil || r.db == nil {
		return biz.Contest{}, status.Error(codes.Internal, "contest database is not configured")
	}
	var c biz.Contest
	var state string
	err := r.db.QueryRowContext(ctx, `SELECT id,title,status,start_at,end_at,created_by,created_at,updated_at FROM contests WHERE id = ?`, id).Scan(&c.ID, &c.Title, &state, &c.StartAt, &c.EndAt, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return biz.Contest{}, status.Error(codes.NotFound, "contest not found")
	}
	if err != nil {
		return biz.Contest{}, err
	}
	c.Status = fromDBStatus(state)
	if c.Status == contestv1.ContestStatus_CONTEST_STATUS_UNSPECIFIED {
		return biz.Contest{}, status.Error(codes.Internal, "invalid contest status")
	}
	if err := scanProblems(ctx, r.db, &c); err != nil {
		return biz.Contest{}, err
	}
	return c, nil
}

func (r *Repository) List(ctx context.Context, page, pageSize int32, filter contestv1.ContestStatus) ([]biz.Contest, int64, error) {
	if r == nil || r.db == nil {
		return nil, 0, status.Error(codes.Internal, "contest database is not configured")
	}
	where, args := "status <> ?", []interface{}{"archived"}
	switch filter {
	case contestv1.ContestStatus_CONTEST_STATUS_DRAFT:
		where, args = "status = 'draft' AND start_at > UTC_TIMESTAMP(3)", nil
	case contestv1.ContestStatus_CONTEST_STATUS_RUNNING:
		where, args = "status = 'draft' AND start_at <= UTC_TIMESTAMP(3) AND end_at > UTC_TIMESTAMP(3)", nil
	case contestv1.ContestStatus_CONTEST_STATUS_ENDED:
		where, args = "status = 'draft' AND end_at <= UTC_TIMESTAMP(3)", nil
	case contestv1.ContestStatus_CONTEST_STATUS_ARCHIVED:
		where, args = "status = 'archived'", nil
	}
	var total int64
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM contests WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	queryArgs := append(append([]interface{}{}, args...), pageSize, (page-1)*pageSize)
	rows, err := r.db.QueryContext(ctx, "SELECT id,title,status,start_at,end_at,created_by,created_at,updated_at FROM contests WHERE "+where+" ORDER BY start_at DESC,id DESC LIMIT ? OFFSET ?", queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]biz.Contest, 0, pageSize)
	for rows.Next() {
		var c biz.Contest
		var state string
		if err := rows.Scan(&c.ID, &c.Title, &state, &c.StartAt, &c.EndAt, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, 0, err
		}
		c.Status = fromDBStatus(state)
		if err := scanProblems(ctx, r.db, &c); err != nil {
			return nil, 0, err
		}
		items = append(items, c)
	}
	return items, total, rows.Err()
}

func (r *Repository) Update(ctx context.Context, contest biz.Contest) (biz.Contest, error) {
	if r == nil || r.db == nil {
		return biz.Contest{}, status.Error(codes.Internal, "contest database is not configured")
	}
	if contest.UpdatedAt.IsZero() {
		return biz.Contest{}, status.Error(codes.InvalidArgument, "contest update token is required")
	}
	err := withContestTx(ctx, r.db, func(tx *sql.Tx) error {
		var currentUpdated, startAt, dbNow time.Time
		var state string
		err := tx.QueryRowContext(ctx, `SELECT updated_at,start_at,status,UTC_TIMESTAMP(3) FROM contests WHERE id=? FOR UPDATE`, contest.ID).Scan(&currentUpdated, &startAt, &state, &dbNow)
		if errors.Is(err, sql.ErrNoRows) {
			return status.Error(codes.NotFound, "contest not found")
		}
		if err != nil {
			return err
		}
		if state != "draft" || !dbNow.Before(startAt) {
			return status.Error(codes.FailedPrecondition, "contest is not editable")
		}
		if !currentUpdated.Equal(contest.UpdatedAt) {
			return status.Error(codes.Aborted, "contest was modified; reload and retry")
		}
		result, err := tx.ExecContext(ctx, `UPDATE contests SET title=?,start_at=?,end_at=?,updated_at=GREATEST(UTC_TIMESTAMP(3),updated_at + INTERVAL 1 MILLISECOND) WHERE id=? AND status='draft' AND updated_at=?`, contest.Title, contest.StartAt, contest.EndAt, contest.ID, contest.UpdatedAt)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return status.Error(codes.Aborted, "contest was modified; reload and retry")
		}
		return replaceProblems(ctx, tx, contest.ID, contest.Problems)
	})
	if err != nil {
		return biz.Contest{}, err
	}
	return r.Get(ctx, contest.ID)
}

func (r *Repository) Archive(ctx context.Context, id int64) (biz.Contest, error) {
	if r == nil || r.db == nil {
		return biz.Contest{}, status.Error(codes.Internal, "contest database is not configured")
	}
	err := withContestTx(ctx, r.db, func(tx *sql.Tx) error {
		var startAt, dbNow time.Time
		var state string
		err := tx.QueryRowContext(ctx, `SELECT start_at,status,UTC_TIMESTAMP(3) FROM contests WHERE id=? FOR UPDATE`, id).Scan(&startAt, &state, &dbNow)
		if errors.Is(err, sql.ErrNoRows) {
			return status.Error(codes.NotFound, "contest not found")
		}
		if err != nil {
			return err
		}
		if state != "draft" || !dbNow.Before(startAt) {
			return status.Error(codes.FailedPrecondition, "contest is not archivable")
		}
		result, err := tx.ExecContext(ctx, `UPDATE contests SET status='archived',updated_at=GREATEST(UTC_TIMESTAMP(3),updated_at + INTERVAL 1 MILLISECOND) WHERE id=? AND status='draft'`, id)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return status.Error(codes.FailedPrecondition, "contest is not archivable")
		}
		return nil
	})
	if err != nil {
		return biz.Contest{}, err
	}
	return r.Get(ctx, id)
}

func replaceProblems(ctx context.Context, tx *sql.Tx, contestID int64, problems []biz.ContestProblem) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM contest_problems WHERE contest_id=?`, contestID); err != nil {
		return err
	}
	for _, p := range problems {
		if _, err := tx.ExecContext(ctx, `INSERT INTO contest_problems (contest_id,problem_id,sort_order,score) VALUES (?,?,?,?)`, contestID, p.ProblemID, p.SortOrder, p.Score); err != nil {
			return err
		}
	}
	return nil
}
func scanProblems(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, c *biz.Contest) error {
	rows, err := q.QueryContext(ctx, `SELECT problem_id,sort_order,score FROM contest_problems WHERE contest_id=? ORDER BY sort_order,problem_id`, c.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var p biz.ContestProblem
		if err := rows.Scan(&p.ProblemID, &p.SortOrder, &p.Score); err != nil {
			return err
		}
		c.Problems = append(c.Problems, p)
	}
	return rows.Err()
}
func toDBStatus(value contestv1.ContestStatus) string {
	switch value {
	case contestv1.ContestStatus_CONTEST_STATUS_RUNNING:
		return "running"
	case contestv1.ContestStatus_CONTEST_STATUS_ENDED:
		return "ended"
	case contestv1.ContestStatus_CONTEST_STATUS_ARCHIVED:
		return "archived"
	default:
		return "draft"
	}
}
func fromDBStatus(value string) contestv1.ContestStatus {
	switch value {
	case "running":
		return contestv1.ContestStatus_CONTEST_STATUS_RUNNING
	case "ended":
		return contestv1.ContestStatus_CONTEST_STATUS_ENDED
	case "archived":
		return contestv1.ContestStatus_CONTEST_STATUS_ARCHIVED
	case "draft":
		return contestv1.ContestStatus_CONTEST_STATUS_DRAFT
	default:
		return contestv1.ContestStatus_CONTEST_STATUS_UNSPECIFIED
	}
}

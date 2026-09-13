package data

import (
	"context"
	"database/sql"
	"errors"

	mysql "github.com/go-sql-driver/mysql"

	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"github.com/viggggil/go_oj_agent/services/problem/internal/biz"
)

func (s *StoreSet) Create(ctx context.Context, problem biz.Problem, tags []string) (created biz.Problem, err error) {
	if s == nil || s.db == nil {
		return biz.Problem{}, biz.ErrorInternal("problem database is not configured")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return biz.Problem{}, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	result, err := tx.ExecContext(ctx, `
		INSERT INTO problems
			(title, slug, description, difficulty, time_limit_ms, memory_limit_kb, status, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3))
	`, problem.Title, problem.Slug, problem.Description, problem.Difficulty.String(),
		problem.TimeLimitMs, problem.MemoryLimitKb, problem.Status.String(), problem.CreatedBy)
	if err != nil {
		return biz.Problem{}, translateMySQLError(err)
	}
	problem.ID, err = result.LastInsertId()
	if err != nil {
		return biz.Problem{}, err
	}

	problem.Tags = make([]biz.Tag, 0, len(tags))
	for _, name := range tags {
		tag, findErr := findOrCreateTag(ctx, tx, name)
		if findErr != nil {
			return biz.Problem{}, findErr
		}
		if _, err = tx.ExecContext(ctx,
			`INSERT INTO problem_tags (problem_id, tag_id) VALUES (?, ?)`, problem.ID, tag.ID,
		); err != nil {
			return biz.Problem{}, translateMySQLError(err)
		}
		problem.Tags = append(problem.Tags, tag)
	}

	if err = tx.QueryRowContext(ctx,
		`SELECT created_at, updated_at FROM problems WHERE id = ?`, problem.ID,
	).Scan(&problem.CreatedAt, &problem.UpdatedAt); err != nil {
		return biz.Problem{}, err
	}
	if err = tx.Commit(); err != nil {
		return biz.Problem{}, err
	}
	return problem, nil
}

func findOrCreateTag(ctx context.Context, tx *sql.Tx, name string) (biz.Tag, error) {
	var tag biz.Tag
	err := tx.QueryRowContext(ctx, `SELECT id, name FROM tags WHERE name = ?`, name).Scan(&tag.ID, &tag.Name)
	if err == nil {
		return tag, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return biz.Tag{}, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO tags (name, created_at) VALUES (?, UTC_TIMESTAMP(3))`, name)
	if err != nil {
		return biz.Tag{}, translateMySQLError(err)
	}
	tag.ID, err = result.LastInsertId()
	tag.Name = name
	return tag, err
}

func translateMySQLError(err error) error {
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return biz.ErrorAlreadyExists("problem or tag already exists")
	}
	return err
}

func (s *StoreSet) FindByID(ctx context.Context, problemID int64) (biz.Problem, error) {
	if s == nil || s.db == nil {
		return biz.Problem{}, biz.ErrorInternal("problem database is not configured")
	}
	var problem biz.Problem
	var difficulty, problemStatus string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, title, slug, description, difficulty, time_limit_ms, memory_limit_kb,
		       status, created_by, created_at, updated_at
		FROM problems WHERE id = ?
	`, problemID).Scan(&problem.ID, &problem.Title, &problem.Slug, &problem.Description,
		&difficulty, &problem.TimeLimitMs, &problem.MemoryLimitKb, &problemStatus,
		&problem.CreatedBy, &problem.CreatedAt, &problem.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return biz.Problem{}, biz.ErrorNotFound("problem not found")
	}
	if err != nil {
		return biz.Problem{}, err
	}
	difficultyValue, ok := problemv1.ProblemDifficulty_value[difficulty]
	if !ok {
		return biz.Problem{}, biz.ErrorInternal("invalid problem difficulty in database")
	}
	statusValue, ok := problemv1.ProblemStatus_value[problemStatus]
	if !ok {
		return biz.Problem{}, biz.ErrorInternal("invalid problem status in database")
	}
	problem.Difficulty = problemv1.ProblemDifficulty(difficultyValue)
	problem.Status = problemv1.ProblemStatus(statusValue)
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.name
		FROM tags t JOIN problem_tags pt ON pt.tag_id = t.id
		WHERE pt.problem_id = ? ORDER BY t.id
	`, problemID)
	if err != nil {
		return biz.Problem{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var tag biz.Tag
		if err := rows.Scan(&tag.ID, &tag.Name); err != nil {
			return biz.Problem{}, err
		}
		problem.Tags = append(problem.Tags, tag)
	}
	return problem, rows.Err()
}

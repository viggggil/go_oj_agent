package data

import (
	"context"
	"database/sql"
	"errors"

	mysql "github.com/go-sql-driver/mysql"

	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"github.com/viggggil/go_oj_agent/services/problem/internal/biz"
)

func (s *StoreSet) ListTestcases(ctx context.Context, problemID int64, includeArchived bool) ([]biz.Testcase, error) {
	where := " WHERE problem_id = ? AND status = ?"
	args := []interface{}{problemID, problemv1.TestcaseStatus_TESTCASE_STATUS_ACTIVE.String()}
	if includeArchived {
		where = " WHERE problem_id = ?"
		args = []interface{}{problemID}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, problem_id, case_no, input_object_key, output_object_key,
		input_sha256, output_sha256, input_size_bytes, output_size_bytes, status, created_at, archived_at
		FROM testcases`+where+` ORDER BY case_no`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]biz.Testcase, 0)
	for rows.Next() {
		var item biz.Testcase
		var status string
		var archived sql.NullTime
		if err := rows.Scan(&item.ID, &item.ProblemID, &item.CaseNo, &item.InputObjectKey, &item.OutputObjectKey,
			&item.InputSHA256, &item.OutputSHA256, &item.InputSizeBytes, &item.OutputSizeBytes, &status, &item.CreatedAt, &archived); err != nil {
			return nil, err
		}
		value, ok := problemv1.TestcaseStatus_value[status]
		if !ok {
			return nil, biz.ErrorInternal("invalid testcase status in database")
		}
		item.Status = problemv1.TestcaseStatus(value)
		if archived.Valid {
			item.ArchivedAt = &archived.Time
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *StoreSet) CommitAddedTestcase(ctx context.Context, testcase biz.Testcase, activeRevision, expectedActiveRevision string) (created biz.Testcase, err error) {
	if s == nil || s.db == nil {
		return biz.Testcase{}, biz.ErrorInternal("problem database is not configured")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return biz.Testcase{}, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if err = lockPublishableProblem(ctx, tx, testcase.ProblemID, expectedActiveRevision); err != nil {
		return biz.Testcase{}, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO testcases
		(problem_id, case_no, input_object_key, output_object_key, input_sha256, output_sha256,
		 input_size_bytes, output_size_bytes, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3))`,
		testcase.ProblemID, testcase.CaseNo, testcase.InputObjectKey, testcase.OutputObjectKey, testcase.InputSHA256, testcase.OutputSHA256, testcase.InputSizeBytes, testcase.OutputSizeBytes, testcase.Status.String())
	if err != nil {
		return biz.Testcase{}, translateTestcaseMySQLError(err)
	}
	testcase.ID, err = result.LastInsertId()
	if err != nil {
		return biz.Testcase{}, err
	}
	if err = activateRevision(ctx, tx, testcase.ProblemID, activeRevision); err != nil {
		return biz.Testcase{}, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT created_at FROM testcases WHERE id = ?`, testcase.ID).Scan(&testcase.CreatedAt); err != nil {
		return biz.Testcase{}, err
	}
	if err = tx.Commit(); err != nil {
		return biz.Testcase{}, err
	}
	return testcase, nil
}

func (s *StoreSet) CommitArchivedTestcase(ctx context.Context, problemID, testcaseID int64, activeRevision, expectedActiveRevision string) (archived biz.Testcase, err error) {
	if s == nil || s.db == nil {
		return biz.Testcase{}, biz.ErrorInternal("problem database is not configured")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return biz.Testcase{}, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if err = lockPublishableProblem(ctx, tx, problemID, expectedActiveRevision); err != nil {
		return biz.Testcase{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE testcases SET status = ?, archived_at = COALESCE(archived_at, UTC_TIMESTAMP(3)), updated_at = UTC_TIMESTAMP(3) WHERE id = ? AND problem_id = ? AND status = ?`, problemv1.TestcaseStatus_TESTCASE_STATUS_ARCHIVED.String(), testcaseID, problemID, problemv1.TestcaseStatus_TESTCASE_STATUS_ACTIVE.String())
	if err != nil {
		return biz.Testcase{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return biz.Testcase{}, err
	}
	if affected != 1 {
		return biz.Testcase{}, biz.ErrorTestcaseNotFound("active testcase not found")
	}
	if err = activateRevision(ctx, tx, problemID, activeRevision); err != nil {
		return biz.Testcase{}, err
	}
	if err = scanTestcase(tx.QueryRowContext(ctx, `SELECT id, problem_id, case_no, input_object_key, output_object_key,
		input_sha256, output_sha256, input_size_bytes, output_size_bytes, status, created_at, archived_at
		FROM testcases WHERE id = ? AND problem_id = ?`, testcaseID, problemID), &archived); err != nil {
		return biz.Testcase{}, err
	}
	if err = tx.Commit(); err != nil {
		return biz.Testcase{}, err
	}
	return archived, nil
}

func lockPublishableProblem(ctx context.Context, tx *sql.Tx, problemID int64, expectedActiveRevision string) error {
	var status string
	var active sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT status, active_judge_revision FROM problems WHERE id = ? FOR UPDATE`, problemID).Scan(&status, &active)
	if errors.Is(err, sql.ErrNoRows) {
		return biz.ErrorNotFound("problem not found")
	}
	if err != nil {
		return err
	}
	if status != problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL.String() {
		return biz.ErrorInvalidStatus("archived problem testcases cannot be changed")
	}
	if active.String != expectedActiveRevision {
		return biz.ErrorInvalidStatus("active judge revision changed; retry the testcase update")
	}
	return nil
}

func activateRevision(ctx context.Context, tx *sql.Tx, problemID int64, activeRevision string) error {
	var revision interface{}
	if activeRevision != "" {
		revision = activeRevision
	}
	result, err := tx.ExecContext(ctx, `UPDATE problems SET active_judge_revision = ?, updated_at = UTC_TIMESTAMP(3) WHERE id = ?`, revision, problemID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return biz.ErrorNotFound("problem not found")
	}
	return nil
}

type rowScanner interface {
	Scan(...interface{}) error
}

func scanTestcase(row rowScanner, item *biz.Testcase) error {
	var status string
	var archived sql.NullTime
	if err := row.Scan(&item.ID, &item.ProblemID, &item.CaseNo, &item.InputObjectKey, &item.OutputObjectKey,
		&item.InputSHA256, &item.OutputSHA256, &item.InputSizeBytes, &item.OutputSizeBytes, &status, &item.CreatedAt, &archived); err != nil {
		return err
	}
	value, ok := problemv1.TestcaseStatus_value[status]
	if !ok {
		return biz.ErrorInternal("invalid testcase status in database")
	}
	item.Status = problemv1.TestcaseStatus(value)
	if archived.Valid {
		item.ArchivedAt = &archived.Time
	}
	return nil
}

func translateTestcaseMySQLError(err error) error {
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return biz.ErrorTestcaseAlreadyExists("testcase case number already exists")
	}
	return err
}

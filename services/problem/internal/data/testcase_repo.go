package data

import (
	"context"
	"database/sql"
	"errors"

	mysql "github.com/go-sql-driver/mysql"

	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"github.com/viggggil/go_oj_agent/services/problem/internal/biz"
)

func (s *StoreSet) AddTestcase(ctx context.Context, testcase biz.Testcase) (biz.Testcase, error) {
	if s == nil || s.db == nil {
		return biz.Testcase{}, biz.ErrorInternal("problem database is not configured")
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO testcases
		(problem_id, case_no, input_object_key, output_object_key, input_sha256, output_sha256,
		 input_size_bytes, output_size_bytes, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3))`,
		testcase.ProblemID, testcase.CaseNo, testcase.InputObjectKey, testcase.OutputObjectKey, testcase.InputSHA256, testcase.OutputSHA256, testcase.InputSizeBytes, testcase.OutputSizeBytes, testcase.Status.String())
	if err != nil {
		var mysqlErr *mysql.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			return biz.Testcase{}, biz.ErrorTestcaseAlreadyExists("testcase case number already exists")
		}
		return biz.Testcase{}, err
	}
	testcase.ID, err = result.LastInsertId()
	if err != nil {
		return biz.Testcase{}, err
	}
	err = s.db.QueryRowContext(ctx, `SELECT created_at FROM testcases WHERE id = ?`, testcase.ID).Scan(&testcase.CreatedAt)
	return testcase, err
}

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

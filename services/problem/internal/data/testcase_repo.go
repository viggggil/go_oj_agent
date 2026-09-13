package data

import (
	"context"
	"errors"
	mysql "github.com/go-sql-driver/mysql"

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

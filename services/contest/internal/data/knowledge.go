package data

import (
	"context"
	"strings"
)

// 使用数据库时钟；未来和进行中的比赛均阻止题解，管理员也不例外。
func (r *Repository) KnowledgeAllowedProblems(ctx context.Context, ids []int64) ([]int64, error) {
	marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for index, id := range ids {
		args[index] = id
	}
	rows, err := r.db.QueryContext(ctx, `SELECT DISTINCT cp.problem_id FROM contest_problems cp JOIN contests c ON c.id=cp.contest_id WHERE cp.problem_id IN (`+marks+`) AND c.status <> 'archived' AND c.end_at > UTC_TIMESTAMP(3)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	blocked := make(map[int64]bool)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		blocked[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	allowed := make([]int64, 0, len(ids))
	for _, id := range ids {
		if !blocked[id] {
			allowed = append(allowed, id)
		}
	}
	return allowed, nil
}

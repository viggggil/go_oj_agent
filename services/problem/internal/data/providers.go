package data

import (
	"context"
	"database/sql"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"github.com/google/wire"
	"github.com/viggggil/go_oj_agent/services/problem/internal/biz"
	"github.com/viggggil/go_oj_agent/services/problem/internal/conf"
)

var ProviderSet = wire.NewSet(NewMySQLDB, NewStoreSet, wire.Bind(new(biz.ProblemRepository), new(*StoreSet)))

func NewMySQLDB(c *conf.Bootstrap) (*sql.DB, func(), error) {
	if c == nil || c.GetData() == nil || c.GetData().GetMysqlDsn() == "" {
		return nil, func() {}, fmt.Errorf("mysql dsn is required")
	}
	db, e := sql.Open("mysql", c.GetData().GetMysqlDsn())
	if e != nil {
		return nil, func() {}, e
	}
	if e = db.PingContext(context.Background()); e != nil {
		db.Close()
		return nil, func() {}, e
	}
	return db, func() { db.Close() }, nil
}

type StoreSet struct{ db *sql.DB }

func NewStoreSet(db *sql.DB) *StoreSet { return &StoreSet{db: db} }
func (s *StoreSet) Create(ctx context.Context, p biz.Problem, tags []string) (out biz.Problem, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	r, err := tx.ExecContext(ctx, `INSERT INTO problems(title,slug,description,difficulty,time_limit_ms,memory_limit_kb,status,created_by,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,UTC_TIMESTAMP(3),UTC_TIMESTAMP(3))`, p.Title, p.Slug, p.Description, p.Difficulty.String(), p.TimeLimitMs, p.MemoryLimitKb, p.Status.String(), p.CreatedBy)
	if err != nil {
		return p, err
	}
	p.ID, err = r.LastInsertId()
	if err != nil {
		return
	}
	for _, name := range tags {
		var id int64
		err = tx.QueryRowContext(ctx, `SELECT id FROM tags WHERE name=?`, name).Scan(&id)
		if err == sql.ErrNoRows {
			rr, e := tx.ExecContext(ctx, `INSERT INTO tags(name,created_at) VALUES(?,UTC_TIMESTAMP(3))`, name)
			if e != nil {
				return p, e
			}
			id, err = rr.LastInsertId()
		} else if err != nil {
			return p, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO problem_tags(problem_id,tag_id) VALUES(?,?)`, p.ID, id); err != nil {
			return p, err
		}
	}
	err = tx.Commit()
	return p, err
}

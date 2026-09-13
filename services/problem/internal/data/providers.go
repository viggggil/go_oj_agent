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

var ProviderSet = wire.NewSet(
	NewMySQLDB,
	NewStoreSet,
	NewMinIOStore,
	wire.Bind(new(biz.ProblemRepository), new(*StoreSet)),
	wire.Bind(new(biz.TestcaseRepository), new(*StoreSet)),
	wire.Bind(new(biz.ObjectStore), new(*MinIOStore)),
)

func NewMySQLDB(config *conf.Bootstrap) (*sql.DB, func(), error) {
	if config == nil || config.GetData() == nil || config.GetData().GetMysqlDsn() == "" {
		return nil, func() {}, fmt.Errorf("mysql dsn is required")
	}
	db, err := sql.Open("mysql", config.GetData().GetMysqlDsn())
	if err != nil {
		return nil, func() {}, err
	}
	cleanup := func() { _ = db.Close() }
	if err := db.PingContext(context.Background()); err != nil {
		cleanup()
		return nil, func() {}, err
	}
	return db, cleanup, nil
}

type StoreSet struct {
	db *sql.DB
}

func NewStoreSet(db *sql.DB) *StoreSet {
	return &StoreSet{db: db}
}

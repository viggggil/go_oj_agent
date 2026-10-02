package data

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/go-sql-driver/mysql"
	"github.com/google/wire"
	"github.com/viggggil/go_oj_agent/services/contest/internal/biz"
	"github.com/viggggil/go_oj_agent/services/contest/internal/conf"
)

var ProviderSet = wire.NewSet(NewClientContext, NewMySQLDB, NewRepository, NewSubmissionClient, ProvideSubmissionCreator, wire.Bind(new(biz.ContestRepository), new(*Repository)))

func NewMySQLDB(config *conf.Bootstrap) (*sql.DB, func(), error) {
	if config == nil || config.GetData() == nil || config.GetData().GetMysqlDsn() == "" {
		return nil, func() {}, fmt.Errorf("contest mysql dsn is required")
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

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

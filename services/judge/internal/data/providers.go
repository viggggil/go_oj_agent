package data

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/go-sql-driver/mysql"
	"github.com/google/wire"

	"github.com/viggggil/go_oj_agent/services/judge/internal/biz"
	"github.com/viggggil/go_oj_agent/services/judge/internal/conf"
)

var ProviderSet = wire.NewSet(
	NewMySQLDB,
	NewStoreSet,
	NewSourceStore,
	NewProblemClient,
	wire.Bind(new(biz.SubmissionRepository), new(*StoreSet)),
	wire.Bind(new(biz.OutboxRepository), new(*StoreSet)),
	wire.Bind(new(biz.JudgeResultRepository), new(*StoreSet)),
	wire.Bind(new(biz.SourceStore), new(*MinIOSourceStore)),
	wire.Bind(new(biz.ProblemCatalog), new(*ProblemClient)),
)

func NewMySQLDB(config *conf.Bootstrap) (*sql.DB, func(), error) {
	if config == nil || config.GetData() == nil || config.GetData().GetMysqlDsn() == "" {
		return nil, func() {}, fmt.Errorf("judge mysql dsn is required")
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

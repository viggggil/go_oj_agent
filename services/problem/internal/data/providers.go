package data

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/go-sql-driver/mysql"
	"github.com/google/wire"
	"github.com/redis/go-redis/v9"

	"github.com/viggggil/go_oj_agent/services/problem/internal/biz"
	"github.com/viggggil/go_oj_agent/services/problem/internal/conf"
)

var ProviderSet = wire.NewSet(
	NewMySQLDB,
	NewStoreSet,
	NewMinIOStore,
	NewRedisClient,
	NewProblemCache,
	wire.Bind(new(biz.ProblemRepository), new(*StoreSet)),
	wire.Bind(new(biz.TestcaseRepository), new(*StoreSet)),
	wire.Bind(new(biz.ProblemCreationCompensator), new(*StoreSet)),
	wire.Bind(new(biz.ObjectStore), new(*MinIOStore)),
	wire.Bind(new(biz.ProblemCache), new(*RedisProblemCache)),
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

func NewRedisClient(config *conf.Bootstrap) (*redis.Client, func(), error) {
	if config == nil || config.GetData() == nil || config.GetData().GetRedisAddr() == "" {
		return nil, func() {}, fmt.Errorf("redis address is required")
	}
	client := redis.NewClient(&redis.Options{Addr: config.GetData().GetRedisAddr(), Password: config.GetData().GetRedisPassword(), DB: int(config.GetData().GetRedisDb())})
	cleanup := func() { _ = client.Close() }
	if err := client.Ping(context.Background()).Err(); err != nil {
		cleanup()
		return nil, func() {}, err
	}
	return client, cleanup, nil
}

type StoreSet struct {
	db *sql.DB
}

func NewStoreSet(db *sql.DB) *StoreSet {
	return &StoreSet{db: db}
}

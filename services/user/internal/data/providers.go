package data

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/go-sql-driver/mysql"
	"github.com/google/wire"
	"github.com/redis/go-redis/v9"

	"github.com/viggggil/go_oj_agent/services/user/internal/biz"
	"github.com/viggggil/go_oj_agent/services/user/internal/conf"
)

var ProviderSet = wire.NewSet(
	NewMySQLDB,
	NewRedisClient,
	NewStoreSet,
	NewRefreshTokenStore,
	wire.Bind(new(biz.UserRepository), new(*StoreSet)),
	wire.Bind(new(biz.RoleRepository), new(*StoreSet)),
	wire.Bind(new(biz.RefreshTokenStore), new(*RedisRefreshTokenStore)),
)

// NewMySQLDB 创建 user-service 使用的 MySQL 连接池。
func NewMySQLDB(config *conf.Bootstrap) (*sql.DB, func(), error) {
	if config == nil || config.GetData() == nil || config.GetData().GetMysqlDsn() == "" {
		return nil, func() {}, fmt.Errorf("mysql dsn is required")
	}
	db, err := sql.Open("mysql", config.GetData().GetMysqlDsn())
	if err != nil {
		return nil, func() {}, err
	}
	cleanup := func() {
		_ = db.Close()
	}
	if err := db.PingContext(context.Background()); err != nil {
		cleanup()
		return nil, func() {}, err
	}
	return db, cleanup, nil
}

// NewRedisClient 创建 refresh token 使用的 Redis 客户端。
func NewRedisClient(config *conf.Bootstrap) (*redis.Client, func(), error) {
	if config == nil || config.GetData() == nil || config.GetData().GetRedisAddr() == "" {
		return nil, func() {}, fmt.Errorf("redis address is required")
	}
	client := redis.NewClient(&redis.Options{
		Addr:     config.GetData().GetRedisAddr(),
		Password: config.GetData().GetRedisPassword(),
		DB:       int(config.GetData().GetRedisDb()),
	})
	cleanup := func() {
		_ = client.Close()
	}
	if err := client.Ping(context.Background()).Err(); err != nil {
		cleanup()
		return nil, func() {}, err
	}
	return client, cleanup, nil
}

func NewRefreshTokenStore(client *redis.Client, config *conf.Bootstrap) *RedisRefreshTokenStore {
	if config == nil || config.GetData() == nil {
		return NewRedisRefreshTokenStore(client, "", nil)
	}
	return NewRedisRefreshTokenStoreWithClock(client, config.GetData().GetRedisNamespace())
}

func NewRedisRefreshTokenStoreWithClock(client *redis.Client, namespace string) *RedisRefreshTokenStore {
	return NewRedisRefreshTokenStore(client, namespace, nil)
}

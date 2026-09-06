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
func NewMySQLDB(config *conf.Config) (*sql.DB, func(), error) {
	if config == nil || config.Data.MySQLDSN == "" {
		return nil, func() {}, fmt.Errorf("mysql dsn is required")
	}
	db, err := sql.Open("mysql", config.Data.MySQLDSN)
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
func NewRedisClient(config *conf.Config) (*redis.Client, func(), error) {
	if config == nil || config.Data.RedisAddr == "" {
		return nil, func() {}, fmt.Errorf("redis address is required")
	}
	client := redis.NewClient(&redis.Options{
		Addr:     config.Data.RedisAddr,
		Password: config.Data.RedisPassword,
		DB:       config.Data.RedisDB,
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

func NewRefreshTokenStore(client *redis.Client, config *conf.Config) *RedisRefreshTokenStore {
	if config == nil {
		return NewRedisRefreshTokenStore(client, "", nil)
	}
	return NewRedisRefreshTokenStoreWithClock(client, config.Data.RedisNamespace)
}

func NewRedisRefreshTokenStoreWithClock(client *redis.Client, namespace string) *RedisRefreshTokenStore {
	return NewRedisRefreshTokenStore(client, namespace, nil)
}

package data

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/redis/go-redis/v9"
	"github.com/viggggil/go_oj_agent/services/problem/internal/biz"
	"github.com/viggggil/go_oj_agent/services/problem/internal/conf"
	"strings"
	"time"
)

type RedisProblemCache struct {
	client    *redis.Client
	namespace string
	ttl       time.Duration
}

func NewProblemCache(client *redis.Client, config *conf.Bootstrap) *RedisProblemCache {
	namespace := "go_oj_agent:problem"
	if config != nil && config.GetData() != nil && strings.TrimSpace(config.GetData().GetRedisNamespace()) != "" {
		namespace = strings.TrimSuffix(config.GetData().GetRedisNamespace(), ":")
	}
	return &RedisProblemCache{client: client, namespace: namespace, ttl: 10 * time.Minute}
}
func (c *RedisProblemCache) key(id int64) string {
	return fmt.Sprintf("%s:problem:%d", c.namespace, id)
}
func (c *RedisProblemCache) Get(ctx context.Context, id int64) (biz.Problem, bool, error) {
	value, err := c.client.Get(ctx, c.key(id)).Bytes()
	if err == redis.Nil {
		return biz.Problem{}, false, nil
	}
	if err != nil {
		return biz.Problem{}, false, err
	}
	var p biz.Problem
	if err := json.Unmarshal(value, &p); err != nil {
		return biz.Problem{}, false, err
	}
	return p, true, nil
}
func (c *RedisProblemCache) Set(ctx context.Context, p biz.Problem) error {
	value, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, c.key(p.ID), value, c.ttl).Err()
}
func (c *RedisProblemCache) Delete(ctx context.Context, id int64) error {
	return c.client.Del(ctx, c.key(id)).Err()
}

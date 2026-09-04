package data

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/viggggil/go_oj_agent/services/user/internal/biz"
)

const defaultRedisNamespace = "go_oj_agent:user"

type RedisRefreshTokenStore struct {
	client    *redis.Client
	namespace string
	now       func() time.Time
}

func NewRedisRefreshTokenStore(
	client *redis.Client,
	namespace string,
	now func() time.Time,
) *RedisRefreshTokenStore {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		namespace = defaultRedisNamespace
	}
	if now == nil {
		now = time.Now
	}
	return &RedisRefreshTokenStore{
		client:    client,
		namespace: namespace,
		now:       now,
	}
}

func (s *RedisRefreshTokenStore) Save(ctx context.Context, record biz.RefreshTokenRecord) error {
	if s == nil || s.client == nil || record.TokenHash == "" || record.SessionID == "" {
		return biz.ErrRefreshTokenDenied
	}
	return s.save(ctx, record)
}

func (s *RedisRefreshTokenStore) FindByHash(ctx context.Context, tokenHash string) (biz.RefreshTokenRecord, error) {
	if s == nil || s.client == nil || tokenHash == "" {
		return biz.RefreshTokenRecord{}, biz.ErrRefreshTokenDenied
	}

	raw, err := s.client.Get(ctx, s.tokenKey(tokenHash)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return biz.RefreshTokenRecord{}, biz.ErrRefreshTokenDenied
		}
		return biz.RefreshTokenRecord{}, err
	}

	var record biz.RefreshTokenRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return biz.RefreshTokenRecord{}, err
	}
	return record, nil
}

func (s *RedisRefreshTokenStore) Rotate(
	ctx context.Context,
	oldTokenHash string,
	next biz.RefreshTokenRecord,
) error {
	// 轮换先读取旧记录；如果旧记录已经撤销或锁定，视为异常重放并拒绝继续签发。
	old, err := s.FindByHash(ctx, oldTokenHash)
	if err != nil {
		return err
	}
	if old.Revoked || old.ReplayLocked {
		old.ReplayLocked = true
		_ = s.save(ctx, old)
		return biz.ErrRefreshTokenDenied
	}

	now := s.now().UTC()
	old.Revoked = true
	old.LastUsedAt = now
	next.LastUsedAt = now
	if next.SessionID == "" {
		next.SessionID = old.SessionID
	}
	if next.RotatedFrom == "" {
		next.RotatedFrom = old.TokenID
	}

	if err := s.save(ctx, old); err != nil {
		return err
	}
	return s.save(ctx, next)
}

func (s *RedisRefreshTokenStore) RevokeSession(ctx context.Context, sessionID string) error {
	if s == nil || s.client == nil || sessionID == "" {
		return biz.ErrRefreshTokenDenied
	}

	hashes, err := s.client.SMembers(ctx, s.sessionKey(sessionID)).Result()
	if err != nil {
		return err
	}
	for _, tokenHash := range hashes {
		record, err := s.FindByHash(ctx, tokenHash)
		if err != nil {
			continue
		}
		record.Revoked = true
		record.LastUsedAt = s.now().UTC()
		if err := s.save(ctx, record); err != nil {
			return err
		}
	}
	return nil
}

func (s *RedisRefreshTokenStore) save(ctx context.Context, record biz.RefreshTokenRecord) error {
	ttl := record.ExpiresAt.Sub(s.now().UTC())
	if ttl <= 0 {
		return biz.ErrRefreshTokenDenied
	}

	payload, err := json.Marshal(record)
	if err != nil {
		return err
	}
	pipe := s.client.TxPipeline()
	pipe.Set(ctx, s.tokenKey(record.TokenHash), payload, ttl)
	pipe.SAdd(ctx, s.sessionKey(record.SessionID), record.TokenHash)
	pipe.Expire(ctx, s.sessionKey(record.SessionID), ttl)
	_, err = pipe.Exec(ctx)
	return err
}

func (s *RedisRefreshTokenStore) tokenKey(tokenHash string) string {
	return s.namespace + ":refresh_token:" + tokenHash
}

func (s *RedisRefreshTokenStore) sessionKey(sessionID string) string {
	return s.namespace + ":refresh_session:" + sessionID
}

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

var rotateRefreshTokenScript = redis.NewScript(`
local raw = redis.call("GET", KEYS[1])
if not raw then
  return -1
end

local old = cjson.decode(raw)
local old_ttl = redis.call("PTTL", KEYS[1])
if old_ttl <= 0 then
  return -1
end

if old.Revoked == true or old.ReplayLocked == true then
  old.ReplayLocked = true
  redis.call("SET", KEYS[1], cjson.encode(old), "PX", old_ttl)
  return 0
end

local next = cjson.decode(ARGV[1])
old.Revoked = true
old.LastUsedAt = ARGV[2]
next.LastUsedAt = ARGV[2]

local session_key = KEYS[3]
if not next.SessionID or next.SessionID == "" then
  next.SessionID = old.SessionID
  session_key = ARGV[4] .. old.SessionID
end
if not next.RotatedFrom or next.RotatedFrom == "" then
  next.RotatedFrom = old.TokenID
end

redis.call("SET", KEYS[1], cjson.encode(old), "PX", old_ttl)
redis.call("SET", KEYS[2], cjson.encode(next), "PX", ARGV[3])
redis.call("SADD", session_key, next.TokenHash)
redis.call("PEXPIRE", session_key, ARGV[3])
return 1
`)

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
	if s == nil || s.client == nil || oldTokenHash == "" ||
		next.TokenHash == "" {
		return biz.ErrRefreshTokenDenied
	}

	now := s.now().UTC()
	next.LastUsedAt = now
	nextTTL := next.ExpiresAt.Sub(now)
	if nextTTL <= 0 {
		return biz.ErrRefreshTokenDenied
	}
	nextPayload, err := json.Marshal(next)
	if err != nil {
		return err
	}

	result, err := rotateRefreshTokenScript.Run(
		ctx,
		s.client,
		[]string{
			s.tokenKey(oldTokenHash),
			s.tokenKey(next.TokenHash),
			s.sessionKey(next.SessionID),
		},
		nextPayload,
		now.Format(time.RFC3339Nano),
		nextTTL.Milliseconds(),
		s.namespace+":refresh_session:",
	).Int()
	if err != nil {
		return err
	}
	if result != 1 {
		return biz.ErrRefreshTokenDenied
	}
	return nil
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

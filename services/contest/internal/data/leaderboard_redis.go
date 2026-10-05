package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/viggggil/go_oj_agent/services/contest/internal/biz"
	"github.com/viggggil/go_oj_agent/services/contest/internal/conf"
)

var ErrCacheIncomplete = errors.New("leaderboard generation absent or incomplete; rebuild required")
var ErrGenerationChanged = errors.New("leaderboard active generation changed")

type LeaderboardRedis struct {
	client    redis.UniversalClient
	namespace string
	timeout   time.Duration
	enabled   bool
}

func NewLeaderboardRedis(c *conf.Bootstrap) (*LeaderboardRedis, func(), error) {
	cache := &LeaderboardRedis{}
	cfg := c.GetLeaderboardCache()
	if cfg == nil || !cfg.GetEnabled() {
		return cache, func() {}, nil
	}
	if err := conf.ValidateLeaderboardCache(cfg); err != nil {
		return nil, func() {}, err
	}
	timeout, _ := time.ParseDuration(cfg.GetTimeout())
	cache.enabled = true
	cache.namespace = cfg.GetNamespace()
	cache.timeout = timeout
	// No startup Ping: an unavailable cache must not block the result consumer.
	cache.client = redis.NewUniversalClient(&redis.UniversalOptions{Addrs: cfg.GetAddresses(), Password: cfg.GetPassword(), DB: int(cfg.GetDb()), DialTimeout: timeout, ReadTimeout: timeout, WriteTimeout: timeout, ContextTimeoutEnabled: true, MaxRetries: -1})
	return cache, func() { _ = cache.client.Close() }, nil
}
func (c *LeaderboardRedis) Enabled() bool        { return c != nil && c.enabled }
func (c *LeaderboardRedis) base(id int64) string { return fmt.Sprintf("%s:{%d}:lb:", c.namespace, id) }
func (c *LeaderboardRedis) generationKeys(id int64, generation string) []string {
	prefix := c.base(id) + generation + ":"
	return []string{prefix + "rank", prefix + "entries", prefix + "versions", prefix + "members", prefix + "meta"}
}

// Ranking preserves solved DESC, penalty ASC, user_id ASC without combining
// arbitrary integers into a double. All ZSET scores are zero. The '~' member
// is a generation sentinel, sorted after every numeric ranking member.
func leaderboardMember(s biz.LeaderboardSnapshot) string {
	return fmt.Sprintf("%03d:%019d:%019d", 100-s.SolvedCount, s.PenaltySeconds, s.UserID)
}

var prepareGeneration = redis.NewScript(`
for i=1,5 do if redis.call('EXISTS',KEYS[i])~=0 then return 0 end end
redis.call('HSET',KEYS[5],'generation',ARGV[1],'state','creating')
local function write(command,...)
 local result=redis.pcall(command,...)
 if type(result)=='table' and result.err then return false end
 return true
end
if not write('ZADD',KEYS[1],0,'~') then return -1 end
for i=2,4 do if not write('HSET',KEYS[i],'__generation',ARGV[1]) then return -1 end end
if not write('HSET',KEYS[5],'state','building') then return -1 end
return 1
`)

// PrepareGeneration allocates a fresh build target. It neither publishes it as
// active nor marks it ready. PR3 owns snapshot loading, catch-up and activation.
func (c *LeaderboardRedis) PrepareGeneration(ctx context.Context, id int64, generation string) error {
	if !c.Enabled() {
		return ErrCacheIncomplete
	}
	parsed, err := uuid.Parse(generation)
	if err != nil || parsed.String() != generation || id <= 0 {
		return fmt.Errorf("invalid leaderboard generation")
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	result, err := prepareGeneration.Run(ctx, c.client, c.generationKeys(id, generation), generation).Int()
	if err != nil {
		return err
	}
	if result != 1 {
		return ErrCacheIncomplete
	}
	return nil
}

var applyLeaderboard = redis.NewScript(`
-- Validate types and consistency before the first write: Lua errors do not
-- roll back previous commands. Keep a dirty marker if any write fails.
local expected={'string','zset','hash','hash','hash','hash'}
for i=1,6 do
 if redis.call('TYPE',KEYS[i]).ok~=expected[i] then return -1 end
end
if redis.call('GET',KEYS[1])~=ARGV[1] then return -3 end
if redis.call('HGET',KEYS[6],'generation')~=ARGV[1] then return -1 end
local state=redis.call('HGET',KEYS[6],'state')
if state~='building' and state~='ready' then return -1 end
if redis.call('ZSCORE',KEYS[2],'~')~='0' then return -1 end
local count=redis.call('ZCARD',KEYS[2])
for i=3,5 do
 if redis.call('HGET',KEYS[i],'__generation')~=ARGV[1] or redis.call('HLEN',KEYS[i])~=count then return -1 end
end
local user=ARGV[2]
local incoming=ARGV[3]
if #incoming~=19 or not string.match(incoming,'^%d+$') then return -2 end
local version=redis.call('HGET',KEYS[4],user)
local member=redis.call('HGET',KEYS[5],user)
local entry=redis.call('HGET',KEYS[3],user)
if version then
 if #version~=19 or not string.match(version,'^%d+$') or version<'0000000000000000001' or version>'9223372036854775807' then return -1 end
 if not member or #member~=43 or string.sub(member,25)~=ARGV[6] or not entry or redis.call('ZSCORE',KEYS[2],member)~='0' then return -1 end
 if incoming<=version then return 0 end
elseif member or entry then return -1 end
local function write(command,...)
 local result=redis.pcall(command,...)
 return not (type(result)=='table' and result.err)
end
if not write('HSET',KEYS[6],'state','dirty') then return -1 end
if member and not write('ZREM',KEYS[2],member) then return -1 end
if not write('ZADD',KEYS[2],0,ARGV[4]) then return -1 end
if not write('HSET',KEYS[3],user,ARGV[5]) then return -1 end
if not write('HSET',KEYS[4],user,incoming) then return -1 end
if not write('HSET',KEYS[5],user,ARGV[4]) then return -1 end
if not write('HSET',KEYS[6],'state',state) then return -1 end
return 1
`)

func (c *LeaderboardRedis) Apply(ctx context.Context, s biz.LeaderboardSnapshot) error {
	if err := s.Validate(); err != nil {
		return fmt.Errorf("%w: %v", biz.ErrInvalidSnapshot, err)
	}
	if !c.Enabled() {
		return ErrCacheIncomplete
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	pointer := c.base(s.ContestID) + "active"
	generation, err := c.client.Get(ctx, pointer).Result()
	if errors.Is(err, redis.Nil) {
		return ErrCacheIncomplete
	}
	if err != nil {
		return err
	}
	parsed, err := uuid.Parse(generation)
	if err != nil || parsed.String() != generation {
		return ErrCacheIncomplete
	}
	payload, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("%w: encode snapshot", biz.ErrInvalidSnapshot)
	}
	version, _ := strconv.ParseInt(s.Version, 10, 64)
	keys := append([]string{pointer}, c.generationKeys(s.ContestID, generation)...)
	result, err := applyLeaderboard.Run(ctx, c.client, keys, generation, strconv.FormatInt(s.UserID, 10), fmt.Sprintf("%019d", version), leaderboardMember(s), string(payload), fmt.Sprintf("%019d", s.UserID)).Int()
	if err != nil {
		return err
	}
	switch result {
	case 0, 1:
		return nil
	case -3:
		return ErrGenerationChanged
	case -2:
		return biz.ErrInvalidSnapshot
	default:
		return ErrCacheIncomplete
	}
}

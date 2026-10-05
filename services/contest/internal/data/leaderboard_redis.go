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
<<<<<<< HEAD
redis.call('PEXPIRE',KEYS[5],600000)
=======
>>>>>>> origin/main
local function write(command,...)
 local result=redis.pcall(command,...)
 if type(result)=='table' and result.err then return false end
 return true
end
if not write('ZADD',KEYS[1],0,'~') then return -1 end
<<<<<<< HEAD
redis.call('PEXPIRE',KEYS[1],600000)
for i=2,4 do
 if not write('HSET',KEYS[i],'__generation',ARGV[1]) then return -1 end
 redis.call('PEXPIRE',KEYS[i],600000)
end
if not write('HSET',KEYS[5],'state','building') then return -1 end
for i=1,5 do redis.call('PEXPIRE',KEYS[i],600000) end
=======
for i=2,4 do if not write('HSET',KEYS[i],'__generation',ARGV[1]) then return -1 end end
if not write('HSET',KEYS[5],'state','building') then return -1 end
>>>>>>> origin/main
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

<<<<<<< HEAD
// Both live relay updates and snapshot loading share this exact version fence.
const updateLeaderboardLua = `
local function update(keys,target)
local expected={'zset','hash','hash','hash','hash'}
for i=1,5 do if redis.call('TYPE',keys[i]).ok~=expected[i] then return -1 end end
if redis.call('HGET',keys[5],'generation')~=target then return -1 end
local state=redis.call('HGET',keys[5],'state')
if state~='building' and state~='ready' then return -1 end
if redis.call('ZSCORE',keys[1],'~')~='0' then return -1 end
local count=redis.call('ZCARD',keys[1])
for i=2,4 do
 if redis.call('HGET',keys[i],'__generation')~=target or redis.call('HLEN',keys[i])~=count then return -1 end
=======
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
>>>>>>> origin/main
end
local user=ARGV[2]
local incoming=ARGV[3]
if #incoming~=19 or not string.match(incoming,'^%d+$') then return -2 end
<<<<<<< HEAD
local version=redis.call('HGET',keys[3],user)
local member=redis.call('HGET',keys[4],user)
local entry=redis.call('HGET',keys[2],user)
if version then
 if #version~=19 or not string.match(version,'^%d+$') or version<'0000000000000000001' or version>'9223372036854775807' then return -1 end
 if not member or #member~=43 or string.sub(member,25)~=ARGV[6] or not entry or redis.call('ZSCORE',keys[1],member)~='0' then return -1 end
=======
local version=redis.call('HGET',KEYS[4],user)
local member=redis.call('HGET',KEYS[5],user)
local entry=redis.call('HGET',KEYS[3],user)
if version then
 if #version~=19 or not string.match(version,'^%d+$') or version<'0000000000000000001' or version>'9223372036854775807' then return -1 end
 if not member or #member~=43 or string.sub(member,25)~=ARGV[6] or not entry or redis.call('ZSCORE',KEYS[2],member)~='0' then return -1 end
>>>>>>> origin/main
 if incoming<=version then return 0 end
elseif member or entry then return -1 end
local function write(command,...)
 local result=redis.pcall(command,...)
 return not (type(result)=='table' and result.err)
end
<<<<<<< HEAD
if not write('HSET',keys[5],'state','dirty') then return -1 end
if member and not write('ZREM',keys[1],member) then return -1 end
if not write('ZADD',keys[1],0,ARGV[4]) then return -1 end
if not write('HSET',keys[2],user,ARGV[5]) then return -1 end
if not write('HSET',keys[3],user,incoming) then return -1 end
if not write('HSET',keys[4],user,ARGV[4]) then return -1 end
if not write('HSET',keys[5],'state',state) then return -1 end
return 1
end
`

// Keep the pointer check and all destinations in one EVAL. A worker which read
// pointers before build registration/cutover retries, and must not ACK old-only.
var relayLeaderboard = redis.NewScript(updateLeaderboardLua + `
if (redis.call('GET',KEYS[1]) or '')~=ARGV[1] or (redis.call('GET',KEYS[2]) or '')~=ARGV[7] then return -3 end
if ARGV[1]=='' and ARGV[7]=='' then return -1 end
local good=true
if ARGV[1]~='' then
 local result=update({KEYS[4],KEYS[5],KEYS[6],KEYS[7],KEYS[8]},ARGV[1])
 if result<0 then good=false end
end
if ARGV[7]~='' then
 if redis.call('GET',KEYS[3])~=ARGV[7] then return -3 end
 local result=update({KEYS[9],KEYS[10],KEYS[11],KEYS[12],KEYS[13]},ARGV[7])
 if result<0 then good=false end
end
if not good then return -1 end
return 1
`)

var loadLeaderboard = redis.NewScript(updateLeaderboardLua + `
if redis.call('GET',KEYS[1])~=ARGV[1] or redis.call('GET',KEYS[2])~=ARGV[1] then return -3 end
return update({KEYS[3],KEYS[4],KEYS[5],KEYS[6],KEYS[7]},ARGV[1])
`)

func snapshotArguments(s biz.LeaderboardSnapshot, generation string) ([]any, error) {
	if err := s.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", biz.ErrInvalidSnapshot, err)
	}
	payload, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("%w: encode snapshot", biz.ErrInvalidSnapshot)
	}
	version, _ := strconv.ParseInt(s.Version, 10, 64)
	return []any{generation, strconv.FormatInt(s.UserID, 10), fmt.Sprintf("%019d", version), leaderboardMember(s), string(payload), fmt.Sprintf("%019d", s.UserID)}, nil
}

func scriptUpdateResult(result int, err error) error {
=======
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
>>>>>>> origin/main
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
<<<<<<< HEAD

func (c *LeaderboardRedis) pointers(ctx context.Context, id int64) (string, string, error) {
	values, err := c.client.MGet(ctx, c.base(id)+"active", c.base(id)+"building").Result()
	if err != nil {
		return "", "", err
	}
	var pointers [2]string
	for i, value := range values {
		if value == nil {
			continue
		}
		generation, ok := value.(string)
		parsed, err := uuid.Parse(generation)
		if !ok || err != nil || parsed.String() != generation {
			return "", "", ErrCacheIncomplete
		}
		pointers[i] = generation
	}
	return pointers[0], pointers[1], nil
}

func (c *LeaderboardRedis) Apply(ctx context.Context, s biz.LeaderboardSnapshot) error {
	args, err := snapshotArguments(s, "")
	if err != nil {
		return err
	}
	if !c.Enabled() {
		return ErrCacheIncomplete
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	active, building, err := c.pointers(ctx, s.ContestID)
	if err != nil {
		return err
	}
	args[0] = active
	args = append(args, building)
	// Missing destinations have dummy generation keys in the same Cluster slot;
	// Lua touches them only when their pointer is nonempty.
	keys := []string{c.base(s.ContestID) + "active", c.base(s.ContestID) + "building", c.base(s.ContestID) + "lease"}
	keys = append(keys, c.generationKeys(s.ContestID, active)...)
	keys = append(keys, c.generationKeys(s.ContestID, building)...)
	result, err := relayLeaderboard.Run(ctx, c.client, keys, args...).Int()
	return scriptUpdateResult(result, err)
}

func (c *LeaderboardRedis) Load(ctx context.Context, generation string, s biz.LeaderboardSnapshot) error {
	args, err := snapshotArguments(s, generation)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	keys := append([]string{c.base(s.ContestID) + "lease", c.base(s.ContestID) + "building"}, c.generationKeys(s.ContestID, generation)...)
	result, err := loadLeaderboard.Run(ctx, c.client, keys, args...).Int()
	return scriptUpdateResult(result, err)
}
=======
>>>>>>> origin/main

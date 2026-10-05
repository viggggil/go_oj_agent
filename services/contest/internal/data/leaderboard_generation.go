package data

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	contestv1 "github.com/viggggil/go_oj_agent/api/contest/v1"
	"github.com/viggggil/go_oj_agent/services/contest/internal/biz"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	generationTTL = 10 * time.Minute
	buildLease    = 30 * time.Second
	maxCacheAge   = 30 * time.Second
)

var ErrCacheStale = errors.New("leaderboard freshness verification expired")
var ErrReconcileDue = errors.New("leaderboard periodic reconciliation due")
var ErrBuildBusy = errors.New("leaderboard rebuild already leased")

// This inexpensive structural check also distinguishes an allocated empty
// generation from missing keys. Page-specific JSON/member checks happen below.
const validateGenerationLua = `
local function valid(keys,generation)
 local kinds={'zset','hash','hash','hash','hash'}
 for i=1,5 do if redis.call('TYPE',keys[i]).ok~=kinds[i] then return false end end
 if redis.call('HGET',keys[5],'generation')~=generation or redis.call('ZSCORE',keys[1],'~')~='0' then return false end
 local count=redis.call('ZCARD',keys[1])
 for i=2,4 do
  if redis.call('HGET',keys[i],'__generation')~=generation or redis.call('HLEN',keys[i])~=count then return false end
 end
 return true
end
local function nowms()
 local t=redis.call('TIME')
 return tonumber(t[1])*1000+math.floor(tonumber(t[2])/1000)
end
`

var beginBuild = redis.NewScript(validateGenerationLua + `
local keys={KEYS[3],KEYS[4],KEYS[5],KEYS[6],KEYS[7]}
if not valid(keys,ARGV[1]) or redis.call('HGET',KEYS[7],'state')~='building' then return -1 end
if redis.call('EXISTS',KEYS[1])~=0 then return 0 end
redis.call('SET',KEYS[1],ARGV[1],'PX',ARGV[2])
redis.call('SET',KEYS[2],ARGV[1],'PX',ARGV[2])
local previous=''
if redis.call('TYPE',KEYS[8]).ok=='string' then previous=redis.call('GET',KEYS[8]) end
redis.call('HSET',KEYS[7],'previous',previous)
return 1
`)
var renewBuild = redis.NewScript(validateGenerationLua + `
if redis.call('GET',KEYS[1])~=ARGV[1] or redis.call('GET',KEYS[2])~=ARGV[1] then return -3 end
if not valid({KEYS[3],KEYS[4],KEYS[5],KEYS[6],KEYS[7]},ARGV[1]) or redis.call('HGET',KEYS[7],'state')~='building' then return -1 end
redis.call('PEXPIRE',KEYS[1],ARGV[2]);redis.call('PEXPIRE',KEYS[2],ARGV[2])
for i=3,7 do redis.call('PEXPIRE',KEYS[i],ARGV[3]) end
return 1
`)
var activateBuild = redis.NewScript(validateGenerationLua + `
if redis.call('GET',KEYS[1])~=ARGV[1] or redis.call('GET',KEYS[2])~=ARGV[1] then return -3 end
if not valid({KEYS[4],KEYS[5],KEYS[6],KEYS[7],KEYS[8]},ARGV[1]) or redis.call('HGET',KEYS[8],'state')~='building' then return -1 end
local active=''
if redis.call('TYPE',KEYS[3]).ok=='string' then active=redis.call('GET',KEYS[3]) end
local previous=redis.call('HGET',KEYS[8],'previous')
if active~='' and active~=previous then return -3 end
redis.call('HSET',KEYS[8],'state','ready','signature',ARGV[2],'built_ms',string.format('%.0f',nowms()))
for i=4,8 do redis.call('PEXPIRE',KEYS[i],ARGV[3]) end
redis.call('SET',KEYS[3],ARGV[1],'PX',ARGV[3])
redis.call('DEL',KEYS[2],KEYS[1])
return 1
`)
var abandonBuild = redis.NewScript(`
if redis.call('GET',KEYS[1])~=ARGV[1] then return 0 end
if redis.call('GET',KEYS[2])==ARGV[1] then redis.call('DEL',KEYS[2]) end
redis.call('DEL',KEYS[1])
return 1
`)

func (c *LeaderboardRedis) buildKeys(id int64, generation string) []string {
	return append([]string{c.base(id) + "lease", c.base(id) + "building"}, c.generationKeys(id, generation)...)
}
func (c *LeaderboardRedis) BeginBuild(ctx context.Context, id int64) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	// Avoid allocating orphan targets for every losing instance/poll.
	if exists, err := c.client.Exists(ctx, c.base(id)+"lease").Result(); err != nil {
		return "", err
	} else if exists > 0 {
		return "", ErrBuildBusy
	}
	generation := uuid.NewString()
	if err := c.PrepareGeneration(ctx, id, generation); err != nil {
		return "", err
	}
	keys := append(c.buildKeys(id, generation), c.base(id)+"active")
	result, err := beginBuild.Run(ctx, c.client, keys, generation, buildLease.Milliseconds()).Int()
	if err != nil {
		return "", err
	}
	if result == 0 {
		return "", ErrBuildBusy
	}
	if err := scriptUpdateResult(result, nil); err != nil {
		return "", err
	}
	return generation, nil
}
func (c *LeaderboardRedis) RenewBuild(ctx context.Context, id int64, generation string) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	result, err := renewBuild.Run(ctx, c.client, c.buildKeys(id, generation), generation, buildLease.Milliseconds(), generationTTL.Milliseconds()).Int()
	return scriptUpdateResult(result, err)
}
func (c *LeaderboardRedis) ActivateBuild(ctx context.Context, id int64, generation, signature string) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	keys := append([]string{c.base(id) + "lease", c.base(id) + "building", c.base(id) + "active"}, c.generationKeys(id, generation)...)
	result, err := activateBuild.Run(ctx, c.client, keys, generation, signature, generationTTL.Milliseconds()).Int()
	return scriptUpdateResult(result, err)
}
func (c *LeaderboardRedis) AbandonBuild(ctx context.Context, id int64, generation string) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	return abandonBuild.Run(ctx, c.client, []string{c.base(id) + "lease", c.base(id) + "building"}, generation).Err()
}

var verifyFreshness = redis.NewScript(validateGenerationLua + `
if redis.call('GET',KEYS[1])~=ARGV[1] then return -3 end
if not valid({KEYS[2],KEYS[3],KEYS[4],KEYS[5],KEYS[6]},ARGV[1]) or redis.call('HGET',KEYS[6],'state')~='ready' then return -1 end
if redis.call('HGET',KEYS[6],'signature')~=ARGV[2] then return -1 end
redis.call('HSET',KEYS[6],'checked_ms',string.format('%.0f',nowms()),'lag_ms',ARGV[3])
for i=1,6 do redis.call('PEXPIRE',KEYS[i],ARGV[4]) end
local built=tonumber(redis.call('HGET',KEYS[6],'built_ms'))
if not built or nowms()-built>300000 then return 2 end
return 1
`)

func (c *LeaderboardRedis) VerifyFreshness(ctx context.Context, id int64, generation, signature string, lag time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	keys := append([]string{c.base(id) + "active"}, c.generationKeys(id, generation)...)
	result, err := verifyFreshness.Run(ctx, c.client, keys, generation, signature, lag.Milliseconds(), generationTTL.Milliseconds()).Int()
	if err == nil && result == 2 {
		return ErrReconcileDue
	}
	return scriptUpdateResult(result, err)
}

var readLeaderboard = redis.NewScript(validateGenerationLua + `
if redis.call('GET',KEYS[1])~=ARGV[1] then return {-3} end
if not valid({KEYS[2],KEYS[3],KEYS[4],KEYS[5],KEYS[6]},ARGV[1]) or redis.call('HGET',KEYS[6],'state')~='ready' then return {-1} end
if redis.call('HGET',KEYS[6],'signature')~=ARGV[2] then return {-1} end
local checked=tonumber(redis.call('HGET',KEYS[6],'checked_ms'))
local lag=tonumber(redis.call('HGET',KEYS[6],'lag_ms'))
local now=nowms()
if not checked or not lag or lag<0 or now<checked or now-checked+lag>tonumber(ARGV[5]) then return {-4} end
local total=redis.call('ZCARD',KEYS[2])-1
local offset=tonumber(ARGV[3]);local size=tonumber(ARGV[4])
if offset<0 or size<1 or size>100 then return {-1} end
local out={total}
if offset>=total then return out end
local members=redis.call('ZRANGE',KEYS[2],offset,math.min(offset+size-1,total-1))
for _,member in ipairs(members) do
 if #member~=43 or not string.match(member,'^%d%d%d:%d+:%d+$') then return {-1} end
 local user=string.gsub(string.sub(member,25),'^0+','')
 local entry=redis.call('HGET',KEYS[3],user)
 local version=redis.call('HGET',KEYS[4],user)
 if not entry or not version or redis.call('HGET',KEYS[5],user)~=member or redis.call('ZSCORE',KEYS[2],member)~='0' then return {-1} end
 table.insert(out,member);table.insert(out,entry);table.insert(out,version)
end
return out
`)

func contestSignature(contest biz.Contest) string {
	var spec strings.Builder
	fmt.Fprintf(&spec, "%d/%d/%d", contest.ID, contest.StartAt.UnixMilli(), contest.EndAt.UnixMilli())
	for _, p := range contest.Problems {
		fmt.Fprintf(&spec, "/%d:%d:%d", p.ProblemID, p.SortOrder, p.Score)
	}
	digest := sha256.Sum256([]byte(spec.String()))
	return hex.EncodeToString(digest[:])
}
func (c *LeaderboardRedis) Read(ctx context.Context, contest biz.Contest, page, size int32) ([]*contestv1.LeaderboardEntry, int64, error) {
	if page <= 0 || size <= 0 || size > 100 {
		return nil, 0, fmt.Errorf("invalid leaderboard page")
	}
	if !c.Enabled() {
		recordCacheMiss()
		return nil, 0, ErrCacheIncomplete
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	generation, _, err := c.pointers(ctx, contest.ID)
	if err != nil {
		recordCacheError()
		return nil, 0, err
	}
	if generation == "" {
		recordCacheMiss()
		return nil, 0, ErrCacheIncomplete
	}
	offset := (int64(page) - 1) * int64(size)
	keys := append([]string{c.base(contest.ID) + "active"}, c.generationKeys(contest.ID, generation)...)
	values, err := readLeaderboard.Run(ctx, c.client, keys, generation, contestSignature(contest), offset, size, maxCacheAge.Milliseconds()).Slice()
	if err != nil {
		recordCacheError()
		return nil, 0, err
	}
	if len(values) == 0 {
		recordCacheMiss()
		return nil, 0, ErrCacheIncomplete
	}
	total, ok := values[0].(int64)
	if !ok {
		return nil, 0, ErrCacheIncomplete
	}
	switch total {
	case -4:
		return nil, 0, ErrCacheStale
	case -3:
		return nil, 0, ErrGenerationChanged
	}
	if total < 0 || (len(values)-1)%3 != 0 {
		recordCacheMiss()
		return nil, 0, ErrCacheIncomplete
	}
	count := int64(size)
	if total-offset < count {
		count = total - offset
	}
	if count < 0 {
		count = 0
	}
	if int64((len(values)-1)/3) != count {
		return nil, 0, ErrCacheIncomplete
	}
	items := make([]*contestv1.LeaderboardEntry, 0, count)
	for i := 1; i < len(values); i += 3 {
		member, ok1 := values[i].(string)
		payload, ok2 := values[i+1].(string)
		version, ok3 := values[i+2].(string)
		var s biz.LeaderboardSnapshot
		if !ok1 || !ok2 || !ok3 || json.Unmarshal([]byte(payload), &s) != nil || s.Validate() != nil || s.ContestID != contest.ID || member != leaderboardMember(s) || len(s.Problems) != len(contest.Problems) {
			return nil, 0, ErrCacheIncomplete
		}
		v, _ := strconv.ParseInt(s.Version, 10, 64)
		if version != fmt.Sprintf("%019d", v) {
			return nil, 0, ErrCacheIncomplete
		}
		item := &contestv1.LeaderboardEntry{UserId: s.UserID, Rank: int32(offset + int64(len(items)) + 1), SolvedCount: s.SolvedCount, AcceptedCount: s.SolvedCount, Score: s.SolvedCount, Penalty: s.PenaltySeconds, PenaltySeconds: s.PenaltySeconds}
		for j, p := range s.Problems {
			if p.ProblemID != contest.Problems[j].ProblemID {
				return nil, 0, ErrCacheIncomplete
			}
			result := &contestv1.LeaderboardProblemResult{ProblemId: p.ProblemID, Solved: p.Solved, WrongAttempts: p.WrongAttempts}
			if p.AcceptedAt != nil {
				result.AcceptedAt = timestamppb.New(*p.AcceptedAt)
			}
			item.Problems = append(item.Problems, result)
		}
		items = append(items, item)
	}
	recordCacheHit()
	return items, total, nil
}

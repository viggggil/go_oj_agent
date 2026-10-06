package data

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	contestv1 "github.com/viggggil/go_oj_agent/api/contest/v1"
	"github.com/viggggil/go_oj_agent/services/contest/internal/biz"
	"golang.org/x/sync/singleflight"
	"google.golang.org/protobuf/proto"
)

// Request handlers only mark work; a lifecycle-managed background server
// performs verification/rebuild. Redis failures never hold a database result tx.
type leaderboardCache struct {
	cache    *LeaderboardRedis
	mu       sync.Mutex
	contests map[int64]*contestCacheWork
	recent   map[string]leaderboardPage
	cooldown time.Time
	fallback singleflight.Group
	slots    chan struct{}
}
type contestCacheWork struct {
	seen, checkAt, rebuildAt time.Time
	failures                 int
	repair, running          bool
}
type leaderboardPage struct {
	items []*contestv1.LeaderboardEntry
	total int64
	until time.Time
}

func NewCachedRepository(db *sql.DB, cache *LeaderboardRedis) *Repository {
	r := NewRepository(db)
	if cache.Enabled() {
		r.leaderboard = &leaderboardCache{cache: cache, contests: make(map[int64]*contestCacheWork), recent: make(map[string]leaderboardPage), slots: make(chan struct{}, 16)}
	}
	return r
}
func (r *Repository) CachedLeaderboard(ctx context.Context, contest biz.Contest, page, size int32) ([]*contestv1.LeaderboardEntry, int64, error) {
	c := r.leaderboard
	if c == nil {
		return r.Leaderboard(ctx, contest.ID, page, size)
	}
	if page <= 0 || size <= 0 || size > 100 {
		return nil, 0, fmt.Errorf("invalid leaderboard page")
	}
	now := time.Now()
	c.mu.Lock()
	work := c.contests[contest.ID]
	if work == nil {
		if len(c.contests) >= 4096 {
			for id, w := range c.contests {
				if now.Sub(w.seen) > time.Minute {
					delete(c.contests, id)
				}
			}
		}
		if len(c.contests) < 4096 {
			work = &contestCacheWork{}
			c.contests[contest.ID] = work
		}
	}
	if work != nil {
		work.seen = now
	}
	canRead := !now.Before(c.cooldown)
	c.mu.Unlock()
	if canRead {
		items, total, err := c.cache.Read(ctx, contest, page, size)
		if err == nil {
			recordCacheHit()
			return items, total, nil
		}
		if errors.Is(err, ErrCacheIncomplete) || errors.Is(err, ErrCacheStale) || errors.Is(err, ErrGenerationChanged) {
			c.mu.Lock()
			if w := c.contests[contest.ID]; w != nil && errors.Is(err, ErrCacheIncomplete) {
				w.repair = true
			}
			// A SQL page cached before this failed Redis read may reflect an older
			// generation. Never serve it after detecting cache corruption/staleness.
			prefix := fmt.Sprintf("%d/", contest.ID)
			for key := range c.recent {
				if strings.HasPrefix(key, prefix) {
					delete(c.recent, key)
				}
			}
			c.mu.Unlock()
		}
		if ctx.Err() != nil {
			return nil, 0, ctx.Err()
		}
		if !errors.Is(err, ErrCacheIncomplete) && !errors.Is(err, ErrCacheStale) && !errors.Is(err, ErrGenerationChanged) {
			recordCacheError()
			c.mu.Lock()
			c.cooldown = time.Now().Add(5 * time.Second)
			c.mu.Unlock()
		}
	}
	recordCacheMiss()
	// Coalesce simultaneous page misses and briefly reuse immutable SQL pages.
	// There is no actor in this key: biz checked authorization before reaching us.
	key := fmt.Sprintf("%d/%d/%d/%s", contest.ID, page, size, contestSignature(contest))
	c.mu.Lock()
	cached, ok := c.recent[key]
	c.mu.Unlock()
	if ok && time.Now().Before(cached.until) {
		return cloneLeaderboard(cached.items), cached.total, nil
	}
	result := c.fallback.DoChan(key, func() (any, error) {
		c.mu.Lock()
		cached, ok := c.recent[key]
		c.mu.Unlock()
		if ok && time.Now().Before(cached.until) {
			return cached, nil
		}
		callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		select {
		case c.slots <- struct{}{}:
			defer func() { <-c.slots }()
		case <-callCtx.Done():
			return nil, callCtx.Err()
		}
		leaderboardSQLFallbacks.Add(1)
		started := time.Now()
		items, total, err := r.Leaderboard(callCtx, contest.ID, page, size)
		if err != nil {
			return nil, err
		}
		value := leaderboardPage{items: items, total: total, until: started.Add(250 * time.Millisecond)}
		c.mu.Lock()
		for k, v := range c.recent {
			if time.Now().After(v.until) {
				delete(c.recent, k)
			}
		}
		if len(c.recent) < 256 {
			c.recent[key] = value
		}
		c.mu.Unlock()
		return value, nil
	})
	select {
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	case result := <-result:
		if result.Err != nil {
			return nil, 0, result.Err
		}
		value := result.Val.(leaderboardPage)
		return cloneLeaderboard(value.items), value.total, nil
	}
}
func cloneLeaderboard(items []*contestv1.LeaderboardEntry) []*contestv1.LeaderboardEntry {
	out := make([]*contestv1.LeaderboardEntry, len(items))
	for i, item := range items {
		out[i] = proto.Clone(item).(*contestv1.LeaderboardEntry)
	}
	return out
}

func (r *Repository) CacheEnabled() bool { return r.leaderboard != nil }

// One scheduler per process; at most two concurrent maintenance tasks. Each
// contest is verified every 5s while requested and reconciled from SQL every 5m.
// Distributed leases allow one snapshot builder across all instances.
func (r *Repository) MaintainLeaderboards(ctx context.Context) {
	c := r.leaderboard
	if c == nil {
		return
	}
	now := time.Now()
	c.mu.Lock()
	if now.Before(c.cooldown) {
		c.mu.Unlock()
		return
	}
	ids := make([]int64, 0, 2)
	for id, w := range c.contests {
		if now.Sub(w.seen) > generationTTL {
			delete(c.contests, id)
			continue
		}
		if w.running || now.Before(w.checkAt) {
			continue
		}
		w.checkAt = now.Add(5 * time.Second)
		w.running = true
		ids = append(ids, id)
		if len(ids) == 2 {
			break
		}
	}
	c.mu.Unlock()
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Go(func() { r.maintainLeaderboard(ctx, id) })
	}
	wg.Wait()
}
func (r *Repository) maintainLeaderboard(ctx context.Context, id int64) {
	c := r.leaderboard
	defer func() {
		c.mu.Lock()
		if w := c.contests[id]; w != nil {
			w.running = false
		}
		c.mu.Unlock()
	}()
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	contest, err := r.Get(checkCtx, id)
	if err != nil {
		cancel()
		return
	}
	active, _, err := c.cache.pointers(checkCtx, id)
	var cacheErr error
	if err == nil && active != "" {
		cacheErr = r.verifyLeaderboard(checkCtx, contest, active)
	} else {
		cacheErr = ErrCacheIncomplete
	}
	cancel()
	if err != nil && !errors.Is(err, ErrCacheIncomplete) {
		c.mu.Lock()
		c.cooldown = time.Now().Add(5 * time.Second)
		c.mu.Unlock()
		return
	}
	c.mu.Lock()
	work := c.contests[id]
	rebuild := work != nil && !time.Now().Before(work.rebuildAt) && (cacheErr != nil || work.repair)
	c.mu.Unlock()
	if !rebuild {
		return
	}
	err = r.RebuildLeaderboard(ctx, id)
	c.mu.Lock()
	defer c.mu.Unlock()
	work = c.contests[id]
	if work == nil {
		return
	}
	if err == nil {
		work.failures = 0
		work.repair = false
		work.rebuildAt = time.Now().Add(5 * time.Second)
	} else if errors.Is(err, ErrBuildBusy) {
		work.rebuildAt = time.Now().Add(5 * time.Second)
	} else {
		work.failures++
		delay := time.Second << min(work.failures, 6)
		work.rebuildAt = time.Now().Add(delay)
		if ctx.Err() == nil {
			slog.Warn("leaderboard rebuild failed", "contest_id", id, "error", err)
		}
	}
}

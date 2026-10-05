package biz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"time"
)

var ErrLeaseLost = errors.New("contest cache outbox lease lost")
var ErrInvalidSnapshot = errors.New("invalid cache outbox snapshot")

type CacheOutboxEvent struct {
	ID        int64
	ContestID int64
	UserID    int64
	Version   string
	Payload   []byte
	Retries   int
}
type CacheOutboxRepository interface {
	ClaimCacheEvent(context.Context, string, time.Duration) (*CacheOutboxEvent, error)
	CompleteCacheEvent(context.Context, int64, string) error
	FailCacheEvent(context.Context, int64, string, time.Duration, bool, string) error
}
type LeaderboardSink interface {
	Apply(context.Context, LeaderboardSnapshot) error
}

type CacheRelay struct {
	Repository CacheOutboxRepository
	Sink       LeaderboardSink
}

// Claim only one event at a time. A fresh token fences expired workers even
// when a single process reclaims the same event after a timeout.
func (r *CacheRelay) RunOnce(ctx context.Context) (bool, error) {
	if r.Repository == nil || r.Sink == nil {
		return false, fmt.Errorf("cache relay dependencies are required")
	}
	token := uuid.NewString()
	event, err := r.Repository.ClaimCacheEvent(ctx, token, 30*time.Second)
	if err != nil || event == nil {
		return false, err
	}
	var snapshot LeaderboardSnapshot
	if err := json.Unmarshal(event.Payload, &snapshot); err != nil || snapshot.Validate() != nil || snapshot.ContestID != event.ContestID || snapshot.UserID != event.UserID || snapshot.Version != event.Version {
		return true, r.Repository.FailCacheEvent(ctx, event.ID, token, 0, true, "invalid leaderboard snapshot")
	}
	callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	err = r.Sink.Apply(callCtx, snapshot)
	cancel()
	if err != nil {
		if ctx.Err() != nil {
			return true, ctx.Err()
		} // lease expiry recovers a cancelled worker
		dead := errors.Is(err, ErrInvalidSnapshot)
		delay := time.Second
		for i := 0; i < event.Retries && delay < time.Minute; i++ {
			delay *= 2
		}
		if delay > time.Minute {
			delay = time.Minute
		}
		reason := err.Error()
		if len(reason) > 255 {
			reason = reason[:255]
		}
		return true, r.Repository.FailCacheEvent(ctx, event.ID, token, delay, dead, reason)
	}
	return true, r.Repository.CompleteCacheEvent(ctx, event.ID, token)
}

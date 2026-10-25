package biz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/viggggil/go_oj_agent/pkg/mq"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"time"
)

var ErrLeaseLost = errors.New("contest cache outbox lease lost")
var ErrInvalidSnapshot = errors.New("invalid cache outbox snapshot")

type CacheOutboxEvent struct {
	ID          int64
	ContestID   int64
	UserID      int64
	Version     string
	TraceParent string
	TraceState  string
	Payload     []byte
	Retries     int
	Age         time.Duration
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
	Observe    func(string, time.Duration)
}

// Claim only one event at a time. A fresh token fences expired workers even
// when a single process reclaims the same event after a timeout.
func (r *CacheRelay) RunOnce(ctx context.Context) (bool, error) {
	if r.Repository == nil || r.Sink == nil {
		return false, fmt.Errorf("cache relay dependencies are required")
	}
	outcome, age := "idle", time.Duration(0)
	started := time.Now()
	defer func() {
		if r.Observe != nil {
			r.Observe(outcome, age+time.Since(started))
		}
	}()
	token := uuid.NewString()
	event, err := r.Repository.ClaimCacheEvent(ctx, token, 30*time.Second)
	if err != nil || event == nil {
		if err != nil {
			outcome = "error"
		}
		return false, err
	}
	age = event.Age
	traceCtx := mq.ContextFromTraceValues(ctx, event.TraceParent, event.TraceState)
	traceCtx, span := otel.Tracer("oj.contest.cache").Start(traceCtx, "contest.cache_outbox.apply")
	span.SetAttributes(attribute.Int64("contest.id", event.ContestID), attribute.Int64("user.id", event.UserID), attribute.String("leaderboard.version", event.Version), attribute.Int("outbox.retry_count", event.Retries))
	defer span.End()
	var snapshot LeaderboardSnapshot
	if err := json.Unmarshal(event.Payload, &snapshot); err != nil || snapshot.Validate() != nil || snapshot.ContestID != event.ContestID || snapshot.UserID != event.UserID || snapshot.Version != event.Version {
		outcome = "dead"
		span.SetStatus(codes.Error, "invalid leaderboard snapshot")
		return true, r.Repository.FailCacheEvent(traceCtx, event.ID, token, 0, true, "invalid leaderboard snapshot")
	}
	callCtx, cancel := context.WithTimeout(traceCtx, 3*time.Second)
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
		outcome = "retry"
		if dead {
			outcome = "dead"
		}
		span.SetStatus(codes.Error, "cache apply failed")
		return true, r.Repository.FailCacheEvent(traceCtx, event.ID, token, delay, dead, reason)
	}
	err = r.Repository.CompleteCacheEvent(traceCtx, event.ID, token)
	outcome = "applied"
	if err != nil {
		outcome = "confirm_error"
		span.SetStatus(codes.Error, "cache outbox completion failed")
	}
	return true, err
}

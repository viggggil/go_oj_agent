package biz

import (
	"context"
	"sort"
	"time"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	contestv1 "github.com/viggggil/go_oj_agent/api/contest/v1"
	"github.com/viggggil/go_oj_agent/pkg/mq"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	grpcCodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type SubmissionFact struct {
	mq.SubmissionJudged
	Invalidated bool
}

type ProblemResult struct {
	ProblemID            int64
	Solved               bool
	WrongAttempts        int32
	AcceptedSubmissionID *int64
	AcceptedAt           *time.Time
	PenaltySeconds       int64
}

type ScoringPolicy interface {
	Rebuild(time.Time, []SubmissionFact) ProblemResult
}
type ACMScoringPolicy struct{}

func (ACMScoringPolicy) Rebuild(start time.Time, facts []SubmissionFact) ProblemResult {
	ordered := append([]SubmissionFact(nil), facts...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].SubmittedAt.Equal(ordered[j].SubmittedAt) {
			return ordered[i].SubmissionID < ordered[j].SubmissionID
		}
		return ordered[i].SubmittedAt.Before(ordered[j].SubmittedAt)
	})
	var result ProblemResult
	for _, fact := range ordered {
		result.ProblemID = fact.ProblemID
		if fact.Invalidated {
			continue
		}
		switch fact.Verdict {
		case "AC":
			id, at := fact.SubmissionID, fact.SubmittedAt
			result.Solved, result.AcceptedSubmissionID, result.AcceptedAt = true, &id, &at
			result.PenaltySeconds = int64(at.Sub(start)/time.Second) + int64(result.WrongAttempts)*1200
			return result
		case "WA", "TLE", "MLE", "RE", "CE":
			result.WrongAttempts++
		}
	}
	return result
}

type ProjectionEvent struct {
	EventID     string
	CausationID string
	TraceParent string
	TraceState  string
	Fact        SubmissionFact
}
type ProjectionRepository interface {
	ApplyProjection(context.Context, ProjectionEvent) error
}
type ProjectionConsumer struct{ repo ProjectionRepository }

func NewProjectionConsumer(repo ProjectionRepository) *ProjectionConsumer {
	return &ProjectionConsumer{repo: repo}
}

func ParseProjectionEvent(body []byte) (ProjectionEvent, error) {
	var value mq.SubmissionJudged
	envelope, err := mq.UnmarshalEnvelope(body, "", &value)
	if err != nil {
		return ProjectionEvent{}, err
	}
	event := ProjectionEvent{EventID: envelope.EventID, CausationID: envelope.CausationID, TraceParent: envelope.TraceParent, TraceState: envelope.TraceState}
	switch envelope.EventType {
	case mq.EventSubmissionJudged:
		if err := value.Validate(); err != nil {
			return ProjectionEvent{}, err
		}
		event.Fact.SubmissionJudged = value
	case mq.EventSubmissionInvalidated:
		var invalid mq.SubmissionInvalidated
		if _, err := mq.UnmarshalEnvelope(body, mq.EventSubmissionInvalidated, &invalid); err != nil {
			return ProjectionEvent{}, err
		}
		if err := invalid.Validate(); err != nil {
			return ProjectionEvent{}, err
		}
		event.Fact = SubmissionFact{SubmissionJudged: mq.SubmissionJudged{SubmissionID: invalid.SubmissionID, ContestID: invalid.ContestID, UserID: invalid.UserID, ProblemID: invalid.ProblemID, SubmittedAt: invalid.SubmittedAt, JudgedAt: invalid.InvalidatedAt, Verdict: "SYSTEM_ERROR"}, Invalidated: true}
	default:
		return ProjectionEvent{}, status.Error(grpcCodes.InvalidArgument, "unsupported contest event")
	}
	return event, nil
}

func (c *ProjectionConsumer) Handle(ctx context.Context, body []byte) error {
	event, err := ParseProjectionEvent(body)
	if err != nil {
		return status.Error(grpcCodes.InvalidArgument, err.Error())
	}
	if !trace.SpanContextFromContext(ctx).IsValid() {
		ctx = mq.ContextFromTraceValues(ctx, event.TraceParent, event.TraceState)
	}
	ctx, span := otel.Tracer("oj.contest.projection").Start(ctx, "contest.projection.apply")
	span.SetAttributes(attribute.String("messaging.event_id", event.EventID), attribute.String("messaging.causation_id", event.CausationID), attribute.Int64("submission.id", event.Fact.SubmissionID), attribute.Int64("contest.id", event.Fact.ContestID))
	defer span.End()
	if event.Fact.ContestID == 0 {
		return nil
	}
	err = c.repo.ApplyProjection(ctx, event)
	if err != nil {
		span.SetStatus(otelcodes.Error, "projection apply failed")
	}
	return err
}

type LeaderboardRepository interface {
	Leaderboard(context.Context, int64, int32, int32) ([]*contestv1.LeaderboardEntry, int64, error)
}

func (u *ContestUsecase) GetLeaderboard(ctx context.Context, actor *commonv1.RequestContext, id int64, page, size int32) ([]*contestv1.LeaderboardEntry, int64, error) {
	if actor == nil || actor.GetUserId() <= 0 {
		return nil, 0, status.Error(grpcCodes.Unauthenticated, "authenticated actor is required")
	}
	if page <= 0 || size <= 0 || size > 100 {
		return nil, 0, status.Error(grpcCodes.InvalidArgument, "invalid page")
	}
	contest, err := u.Get(ctx, actor, id)
	if err != nil {
		return nil, 0, err
	}
	if repo, ok := u.repo.(interface {
		CachedLeaderboard(context.Context, Contest, int32, int32) ([]*contestv1.LeaderboardEntry, int64, error)
	}); ok {
		return repo.CachedLeaderboard(ctx, contest, page, size)
	}
	repo, ok := u.repo.(LeaderboardRepository)
	if !ok {
		return nil, 0, status.Error(grpcCodes.Unimplemented, "leaderboard repository is not configured")
	}
	return repo.Leaderboard(ctx, id, page, size)
}

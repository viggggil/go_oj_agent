package biz

import "context"

type SubmissionRepository interface {
	FindByID(context.Context, int64) (Submission, error)
	List(context.Context, ListFilter) (SubmissionPage, error)
	GetJudgeResult(context.Context, int64) (JudgeResult, error)
	FindIdempotency(context.Context, int64, string, string) (IdempotencyRecord, bool, error)
	CreateWithOutboxAndIdempotency(context.Context, CreateSubmissionCommand) (CreateSubmissionResult, error)
	InvalidateAndRequeueWithOutboxAndIdempotency(context.Context, RejudgeSubmissionCommand) (RejudgeSubmissionResult, error)
}

type SourceStore interface {
	Put(context.Context, string, []byte) (SourceObject, error)
}

type ProblemCatalog interface {
	GetJudgeProfile(context.Context, int64) (JudgeProfile, error)
}

type JudgeResultRepository interface {
	ApplyJudgeResult(context.Context, JudgeResultEvent) error
}

package biz

import (
	"time"

	"github.com/google/uuid"
	"github.com/google/wire"
)

var ProviderSet = wire.NewSet(NewSubmissionUsecase, NewOutboxRelay, NewResultConsumer)

type SubmissionUsecase struct {
	repository SubmissionRepository
	sources    SourceStore
	problems   ProblemCatalog
	now        func() time.Time
	newEventID func() string
}

func NewSubmissionUsecase(repository SubmissionRepository, sources SourceStore, problems ProblemCatalog) *SubmissionUsecase {
	return &SubmissionUsecase{
		repository: repository,
		sources:    sources,
		problems:   problems,
		now:        func() time.Time { return time.Now().UTC() },
		newEventID: uuid.NewString,
	}
}

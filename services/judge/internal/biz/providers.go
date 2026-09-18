package biz

import "github.com/google/wire"

var ProviderSet = wire.NewSet(NewSubmissionUsecase)

type SubmissionUsecase struct {
	repository SubmissionRepository
	sources    SourceStore
	problems   ProblemCatalog
}

func NewSubmissionUsecase(repository SubmissionRepository, sources SourceStore, problems ProblemCatalog) *SubmissionUsecase {
	return &SubmissionUsecase{repository: repository, sources: sources, problems: problems}
}

package service

import (
	"github.com/google/wire"

	submissionv1 "github.com/viggggil/go_oj_agent/api/submission/v1"
	"github.com/viggggil/go_oj_agent/services/judge/internal/biz"
)

const Name = "judge-service"

var ProviderSet = wire.NewSet(NewSubmissionService)

// SubmissionService registers the public contract while business slices are
// implemented incrementally. Embedded handlers return codes.Unimplemented.
type SubmissionService struct {
	submissionv1.UnimplementedSubmissionServiceServer
	uc *biz.SubmissionUsecase
}

func NewSubmissionService(uc *biz.SubmissionUsecase) *SubmissionService {
	return &SubmissionService{uc: uc}
}

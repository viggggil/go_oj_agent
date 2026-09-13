package service

import (
	"github.com/google/wire"

	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"github.com/viggggil/go_oj_agent/services/problem/internal/biz"
)

var ProviderSet = wire.NewSet(NewProblemService)

const Name = "problem-service"

// ProblemService exposes the generated gRPC contract. Endpoint behavior is
// implemented as vertical slices after the service runtime is established.
type ProblemService struct {
	problemv1.UnimplementedProblemServiceServer
	uc *biz.ProblemUsecase
}

func NewProblemService(uc *biz.ProblemUsecase) *ProblemService {
	return &ProblemService{uc: uc}
}

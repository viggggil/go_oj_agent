package biz

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
)

type Testcase struct {
	ID, ProblemID                   int64
	CaseNo                          int32
	InputObjectKey, OutputObjectKey string
	InputSHA256, OutputSHA256       string
	InputSizeBytes, OutputSizeBytes int64
	Status                          problemv1.TestcaseStatus
	CreatedAt                       time.Time
	ArchivedAt                      *time.Time
}

type TestcaseRepository interface {
	AddTestcase(context.Context, Testcase) (Testcase, error)
}
type ProblemCreationCompensator interface {
	DeleteCreatedProblem(context.Context, int64) error
}
type TestcaseContent struct {
	CaseNo int32
	Input  []byte
	Output []byte
}
type ObjectStore interface {
	Put(context.Context, string, []byte) error
	Delete(context.Context, string) error
}

func NewProblemUsecaseWithStore(problems ProblemRepository, testcases TestcaseRepository, objects ObjectStore, compensator ProblemCreationCompensator) *ProblemUsecase {
	return &ProblemUsecase{repo: problems, testcases: testcases, objects: objects, compensator: compensator}
}

func (uc *ProblemUsecase) AddTestcase(ctx context.Context, requestContext *commonv1.RequestContext, problemID int64, caseNo int32, input, output []byte) (Testcase, error) {
	if err := requireAdmin(requestContext); err != nil {
		return Testcase{}, err
	}
	if uc == nil || uc.repo == nil || uc.testcases == nil || uc.objects == nil {
		return Testcase{}, ErrorInternal("testcase dependencies are not configured")
	}
	if problemID <= 0 || caseNo <= 0 || len(input) == 0 || len(output) == 0 {
		return Testcase{}, ErrorInvalidArgument("invalid testcase input")
	}
	if _, err := uc.repo.FindByID(ctx, problemID); err != nil {
		return Testcase{}, err
	}
	return uc.addTestcaseToProblem(ctx, problemID, caseNo, input, output)
}

func (uc *ProblemUsecase) addTestcaseToProblem(ctx context.Context, problemID int64, caseNo int32, input, output []byte) (Testcase, error) {
	inputKey := fmt.Sprintf("problem-%d/input/%d.in", problemID, caseNo)
	outputKey := fmt.Sprintf("problem-%d/output/%d.out", problemID, caseNo)
	if err := uc.objects.Put(ctx, inputKey, input); err != nil {
		return Testcase{}, ErrorStorageUnavailable("upload testcase input: %v", err)
	}
	if err := uc.objects.Put(ctx, outputKey, output); err != nil {
		_ = uc.objects.Delete(ctx, inputKey)
		return Testcase{}, ErrorStorageUnavailable("upload testcase output: %v", err)
	}
	inputHash, outputHash := sha256.Sum256(input), sha256.Sum256(output)
	testcase, err := uc.testcases.AddTestcase(ctx, Testcase{ProblemID: problemID, CaseNo: caseNo, InputObjectKey: inputKey, OutputObjectKey: outputKey, InputSHA256: fmt.Sprintf("%x", inputHash), OutputSHA256: fmt.Sprintf("%x", outputHash), InputSizeBytes: int64(len(input)), OutputSizeBytes: int64(len(output)), Status: problemv1.TestcaseStatus_TESTCASE_STATUS_ACTIVE})
	if err != nil {
		_ = uc.objects.Delete(ctx, inputKey)
		_ = uc.objects.Delete(ctx, outputKey)
		return Testcase{}, err
	}
	return testcase, nil
}

package biz

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/oklog/ulid/v2"
	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	judgecontract "github.com/viggggil/go_oj_agent/pkg/judge"
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
	ListTestcases(context.Context, int64, bool) ([]Testcase, error)
}

type TestcaseChangeCommitter interface {
	CommitAddedTestcase(context.Context, Testcase, string, string) (Testcase, error)
	CommitArchivedTestcase(context.Context, int64, int64, string, string) (Testcase, error)
}

func (uc *ProblemUsecase) ArchiveTestcase(ctx context.Context, requestContext *commonv1.RequestContext, problemID, testcaseID int64) (Testcase, error) {
	if err := requireAdmin(requestContext); err != nil {
		return Testcase{}, err
	}
	if uc == nil || uc.repo == nil || uc.testcases == nil || uc.testcaseCommits == nil || uc.objects == nil {
		return Testcase{}, ErrorInternal("testcase dependencies are not configured")
	}
	unlock := uc.lockProblem(problemID)
	defer unlock()
	problem, err := uc.repo.FindByID(ctx, problemID)
	if err != nil {
		return Testcase{}, err
	}
	if problem.Status != problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL {
		return Testcase{}, ErrorInvalidStatus("archived problem testcases cannot be changed")
	}
	items, err := uc.testcases.ListTestcases(ctx, problemID, false)
	if err != nil {
		return Testcase{}, err
	}
	remaining := make([]Testcase, 0, len(items)-1)
	found := false
	for _, item := range items {
		if item.ID == testcaseID {
			found = true
			continue
		}
		remaining = append(remaining, item)
	}
	if !found {
		return Testcase{}, ErrorTestcaseNotFound("testcase not found")
	}
	if len(remaining) == 0 {
		// Keep the last published immutable snapshot addressable for historical
		// submissions; the current editor state has no active testcase and the
		// active pointer is cleared until a testcase is added.
		return uc.testcaseCommits.CommitArchivedTestcase(ctx, problemID, testcaseID, "", problem.ActiveJudgeRevision)
	}
	revision, err := uc.publishRevision(ctx, problem, remaining)
	if err != nil {
		return Testcase{}, err
	}
	return uc.testcaseCommits.CommitArchivedTestcase(ctx, problemID, testcaseID, revision, problem.ActiveJudgeRevision)
}

func requireTestcaseReader(ctx *commonv1.RequestContext) error {
	if ctx == nil || ctx.GetUserId() <= 0 {
		return ErrorInvalidArgument("invalid request context")
	}
	for _, role := range ctx.GetRoles() {
		if role == RoleAdmin || role == "judge" {
			return nil
		}
	}
	return ErrorPermissionDenied("testcase reader role required")
}

func (uc *ProblemUsecase) ListTestcases(ctx context.Context, requestContext *commonv1.RequestContext, problemID int64, includeArchived bool) ([]Testcase, error) {
	if err := requireTestcaseReader(requestContext); err != nil {
		return nil, err
	}
	if uc == nil || uc.repo == nil || uc.testcases == nil {
		return nil, ErrorInternal("testcase dependencies are not configured")
	}
	if _, err := uc.repo.FindByID(ctx, problemID); err != nil {
		return nil, err
	}
	return uc.testcases.ListTestcases(ctx, problemID, includeArchived)
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
	PutImmutable(context.Context, string, []byte, string) error
	Get(context.Context, string) ([]byte, error)
	Delete(context.Context, string) error
}

func NewProblemUsecaseWithStore(problems ProblemRepository, testcases TestcaseRepository, objects ObjectStore, compensator ProblemCreationCompensator) *ProblemUsecase {
	testcaseCommits, _ := testcases.(TestcaseChangeCommitter)
	return &ProblemUsecase{repo: problems, testcases: testcases, testcaseCommits: testcaseCommits, objects: objects, compensator: compensator}
}

func (uc *ProblemUsecase) AddTestcase(ctx context.Context, requestContext *commonv1.RequestContext, problemID int64, caseNo int32, input, output []byte) (Testcase, error) {
	if err := requireAdmin(requestContext); err != nil {
		return Testcase{}, err
	}
	if uc == nil || uc.repo == nil || uc.testcases == nil || uc.testcaseCommits == nil || uc.objects == nil {
		return Testcase{}, ErrorInternal("testcase dependencies are not configured")
	}
	if problemID <= 0 || caseNo <= 0 || len(input) == 0 || len(output) == 0 {
		return Testcase{}, ErrorInvalidArgument("invalid testcase input")
	}
	unlock := uc.lockProblem(problemID)
	defer unlock()
	problem, err := uc.repo.FindByID(ctx, problemID)
	if err != nil {
		return Testcase{}, err
	}
	if problem.Status != problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL {
		return Testcase{}, ErrorInvalidStatus("archived problem testcases cannot be changed")
	}
	return uc.addTestcaseToProblem(ctx, problem, caseNo, input, output)
}

func (uc *ProblemUsecase) addTestcaseToProblem(ctx context.Context, problem Problem, caseNo int32, input, output []byte) (Testcase, error) {
	uploadID, err := newUploadID()
	if err != nil {
		return Testcase{}, ErrorInternal("generate testcase object key: %v", err)
	}
	inputKey := fmt.Sprintf("problem-%d/testcases/%d/%s.in", problem.ID, caseNo, uploadID)
	outputKey := fmt.Sprintf("problem-%d/testcases/%d/%s.out", problem.ID, caseNo, uploadID)
	if err := uc.objects.PutImmutable(ctx, inputKey, input, "application/octet-stream"); err != nil {
		return Testcase{}, ErrorStorageUnavailable("upload testcase input: %v", err)
	}
	if err := uc.objects.PutImmutable(ctx, outputKey, output, "application/octet-stream"); err != nil {
		_ = uc.objects.Delete(ctx, inputKey)
		return Testcase{}, ErrorStorageUnavailable("upload testcase output: %v", err)
	}
	inputHash, outputHash := sha256.Sum256(input), sha256.Sum256(output)
	testcase := Testcase{ProblemID: problem.ID, CaseNo: caseNo, InputObjectKey: inputKey, OutputObjectKey: outputKey, InputSHA256: fmt.Sprintf("%x", inputHash), OutputSHA256: fmt.Sprintf("%x", outputHash), InputSizeBytes: int64(len(input)), OutputSizeBytes: int64(len(output)), Status: problemv1.TestcaseStatus_TESTCASE_STATUS_ACTIVE}
	items, err := uc.testcases.ListTestcases(ctx, problem.ID, false)
	if err == nil {
		for _, item := range items {
			if item.CaseNo == caseNo {
				err = ErrorTestcaseAlreadyExists("testcase case number already exists")
				break
			}
		}
	}
	if err == nil {
		items = append(items, testcase)
		var revision string
		revision, err = uc.publishRevision(ctx, problem, items)
		if err == nil {
			testcase, err = uc.testcaseCommits.CommitAddedTestcase(ctx, testcase, revision, problem.ActiveJudgeRevision)
		}
	}
	if err != nil {
		_ = uc.objects.Delete(ctx, inputKey)
		_ = uc.objects.Delete(ctx, outputKey)
		return Testcase{}, err
	}
	return testcase, nil
}

func newUploadID() (string, error) {
	value := ulid.Make()
	return hex.EncodeToString(value[:]), nil
}

func (uc *ProblemUsecase) publishRevision(ctx context.Context, problem Problem, items []Testcase) (string, error) {
	if len(items) == 0 {
		return "", ErrorInvalidStatus("cannot publish an empty testcase revision")
	}
	if problem.ID <= 0 || problem.TimeLimitMs <= 0 || problem.MemoryLimitKb <= 0 {
		return "", ErrorInvalidArgument("invalid judge revision limits")
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CaseNo < items[j].CaseNo })
	revisionID := ulid.Make().String()
	prefix := fmt.Sprintf("problem-%d/judge-revisions/%s", problem.ID, revisionID)
	manifest := judgecontract.Manifest{
		ManifestVersion: judgecontract.ManifestVersion,
		ProblemID:       problem.ID,
		JudgeRevision:   revisionID,
		TimeLimitMS:     problem.TimeLimitMs,
		MemoryLimitKB:   problem.MemoryLimitKb,
		Testcases:       make([]judgecontract.Testcase, 0, len(items)),
	}
	for _, item := range items {
		input, err := uc.readVerifiedObject(ctx, item.InputObjectKey, item.InputSHA256, item.InputSizeBytes)
		if err != nil {
			return "", err
		}
		output, err := uc.readVerifiedObject(ctx, item.OutputObjectKey, item.OutputSHA256, item.OutputSizeBytes)
		if err != nil {
			return "", err
		}
		inputKey := fmt.Sprintf("%s/testcases/%d.in", prefix, item.CaseNo)
		outputKey := fmt.Sprintf("%s/testcases/%d.out", prefix, item.CaseNo)
		if err := uc.objects.PutImmutable(ctx, inputKey, input, "application/octet-stream"); err != nil {
			return "", ErrorStorageUnavailable("publish testcase input: %v", err)
		}
		if err := uc.objects.PutImmutable(ctx, outputKey, output, "application/octet-stream"); err != nil {
			return "", ErrorStorageUnavailable("publish testcase output: %v", err)
		}
		manifest.Testcases = append(manifest.Testcases, judgecontract.Testcase{
			CaseNo: item.CaseNo,
			Input: judgecontract.Object{
				ObjectKey: inputKey, SHA256: item.InputSHA256, SizeBytes: item.InputSizeBytes,
			},
			Output: judgecontract.Object{
				ObjectKey: outputKey, SHA256: item.OutputSHA256, SizeBytes: item.OutputSizeBytes,
			},
		})
	}
	if err := manifest.Validate(); err != nil {
		return "", ErrorInternal("validate judge manifest: %v", err)
	}
	content, err := json.Marshal(manifest)
	if err != nil {
		return "", ErrorInternal("marshal judge manifest: %v", err)
	}
	manifestKey, err := judgecontract.ManifestObjectKey(problem.ID, revisionID)
	if err != nil {
		return "", ErrorInternal("build judge manifest key: %v", err)
	}
	if err := uc.objects.PutImmutable(ctx, manifestKey, content, "application/json"); err != nil {
		return "", ErrorStorageUnavailable("publish judge manifest: %v", err)
	}
	return revisionID, nil
}

func (uc *ProblemUsecase) readVerifiedObject(ctx context.Context, key, wantHash string, wantSize int64) ([]byte, error) {
	content, err := uc.objects.Get(ctx, key)
	if err != nil {
		return nil, ErrorStorageUnavailable("read testcase object: %v", err)
	}
	hash := sha256.Sum256(content)
	if int64(len(content)) != wantSize || fmt.Sprintf("%x", hash) != wantHash {
		return nil, ErrorStorageUnavailable("testcase object integrity check failed")
	}
	return content, nil
}

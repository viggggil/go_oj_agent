package biz

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/oklog/ulid/v2"
	submissionv1 "github.com/viggggil/go_oj_agent/api/submission/v1"
)

const (
	MaxSourceSizeBytes int64 = 1 << 20
	MaxObjectKeyLength       = 512
	MaxPageSize              = 100
	DefaultPageSize          = 20
	MaxRetryCount            = 3
	IdempotencyTTL           = 24 * time.Hour
	JudgeQueueDeadline       = 5 * time.Minute

	OperationCreateSubmission  = "CreateSubmission"
	OperationRejudgeSubmission = "RejudgeSubmission"

	EventTypeJudgeRequested        = "judge.requested"
	EventTypeSubmissionInvalidated = "submission.invalidated"
)

type Submission struct {
	ID                int64
	UserID            int64
	ProblemID         int64
	Language          string
	SourceObjectKey   string
	SourceSHA256      string
	SourceSizeBytes   int64
	JudgeRevision     string
	Status            submissionv1.SubmissionStatus
	Verdict           submissionv1.JudgeVerdict
	TimeMS            *int32
	MemoryKB          *int32
	RetryCount        int32
	SystemErrorReason string
	JudgeDeadlineAt   time.Time
	CreatedAt         time.Time
	JudgedAt          *time.Time
	InvalidatedAt     *time.Time
	UpdatedAt         time.Time
}

type CaseResult struct {
	ID           int64
	SubmissionID int64
	CaseNo       int32
	Verdict      submissionv1.JudgeVerdict
	TimeMS       *int32
	MemoryKB     *int32
	Message      string
	CreatedAt    time.Time
}

type JudgeResult struct {
	Submission Submission
	Cases      []CaseResult
}

type JudgeProfile struct {
	ProblemID     int64
	TimeLimitMS   int32
	MemoryLimitKB int32
	JudgeRevision string
}

type Actor struct {
	ID        int64
	Roles     []string
	RequestID string
	TraceID   string
}

type ListFilter struct {
	UserID    int64
	ProblemID int64
	Status    submissionv1.SubmissionStatus
	Language  string
	Page      int32
	PageSize  int32
}

func (f ListFilter) Normalized() ListFilter {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 {
		f.PageSize = DefaultPageSize
	}
	if f.PageSize > MaxPageSize {
		f.PageSize = MaxPageSize
	}
	f.Language = strings.ToLower(strings.TrimSpace(f.Language))
	return f
}

type SubmissionPage struct {
	Items    []Submission
	Page     int32
	PageSize int32
	Total    int64
}

type SourceObject struct {
	Key    string
	SHA256 string
	Size   int64
}

type IdempotencyRequest struct {
	ActorID     int64
	Operation   string
	Key         string
	RequestHash string
	ExpiresAt   time.Time
}

type IdempotencyRecord struct {
	IdempotencyRequest
	Response  []byte
	CreatedAt time.Time
}

type CreateSubmissionCommand struct {
	Submission    Submission
	Idempotency   IdempotencyRequest
	OutboxEventID string
}

type CreateSubmissionResult struct {
	SubmissionID int64                         `json:"submission_id"`
	Status       submissionv1.SubmissionStatus `json:"status"`
	Replayed     bool                          `json:"-"`
}

type RejudgeSubmissionCommand struct {
	SubmissionID             int64
	JudgeRevision            string
	JudgeDeadlineAt          time.Time
	Idempotency              IdempotencyRequest
	InvalidatedOutboxEventID string
	RequestedOutboxEventID   string
}

type RejudgeSubmissionResult struct {
	InvalidatedSubmissionID int64      `json:"invalidated_submission_id"`
	Submission              Submission `json:"submission"`
	Replayed                bool       `json:"-"`
}

type JudgeRequestedPayload struct {
	SubmissionID    int64  `json:"submission_id"`
	ProblemID       int64  `json:"problem_id"`
	Language        string `json:"language"`
	JudgeRevision   string `json:"judge_revision"`
	SourceObjectKey string `json:"source_object_key"`
	SourceSHA256    string `json:"source_sha256"`
	SourceSizeBytes int64  `json:"source_size_bytes"`
}

type SubmissionInvalidatedPayload struct {
	SubmissionID    int64     `json:"submission_id"`
	UserID          int64     `json:"user_id"`
	ProblemID       int64     `json:"problem_id"`
	PreviousVerdict string    `json:"previous_verdict,omitempty"`
	InvalidatedAt   time.Time `json:"invalidated_at"`
}

type CreateSubmissionInput struct {
	Actor          Actor
	ProblemID      int64
	Language       string
	SourceCode     []byte
	IdempotencyKey string
}

func (uc *SubmissionUsecase) Create(ctx context.Context, input CreateSubmissionInput) (CreateSubmissionResult, error) {
	if err := requireActor(input.Actor); err != nil {
		return CreateSubmissionResult{}, err
	}
	if uc == nil || uc.repository == nil || uc.sources == nil || uc.problems == nil {
		return CreateSubmissionResult{}, ErrorInternal("submission dependencies are not configured")
	}
	language := strings.ToLower(strings.TrimSpace(input.Language))
	if input.ProblemID <= 0 || !SupportedLanguage(language) {
		return CreateSubmissionResult{}, ErrorInvalidArgument("invalid submission problem or language")
	}
	if len(input.SourceCode) == 0 || int64(len(input.SourceCode)) > MaxSourceSizeBytes {
		return CreateSubmissionResult{}, ErrorInvalidArgument("source size must be between 1 and %d bytes", MaxSourceSizeBytes)
	}
	now := uc.currentTime()
	idempotency := IdempotencyRequest{
		ActorID:     input.Actor.ID,
		Operation:   OperationCreateSubmission,
		Key:         input.IdempotencyKey,
		RequestHash: createRequestHash(input.ProblemID, language, input.SourceCode),
		ExpiresAt:   now.Add(IdempotencyTTL),
	}
	if err := ValidateIdempotency(idempotency, OperationCreateSubmission, now); err != nil {
		return CreateSubmissionResult{}, err
	}
	if result, found, err := uc.findCreateReplay(ctx, idempotency); err != nil || found {
		return result, err
	}
	profile, err := uc.problems.GetJudgeProfile(ctx, input.ProblemID)
	if err != nil {
		return CreateSubmissionResult{}, err
	}
	if err := validateJudgeProfile(profile, input.ProblemID); err != nil {
		return CreateSubmissionResult{}, err
	}
	source, err := uc.sources.Put(ctx, language, input.SourceCode)
	if err != nil {
		return CreateSubmissionResult{}, err
	}
	command := CreateSubmissionCommand{
		Submission: Submission{
			UserID:          input.Actor.ID,
			ProblemID:       input.ProblemID,
			Language:        language,
			SourceObjectKey: source.Key,
			SourceSHA256:    source.SHA256,
			SourceSizeBytes: source.Size,
			JudgeRevision:   profile.JudgeRevision,
			Status:          submissionv1.SubmissionStatus_SUBMISSION_STATUS_QUEUED,
			JudgeDeadlineAt: now.Add(JudgeQueueDeadline),
		},
		Idempotency:   idempotency,
		OutboxEventID: uc.eventID(),
	}
	return uc.repository.CreateWithOutboxAndIdempotency(ctx, command)
}

func (uc *SubmissionUsecase) Get(ctx context.Context, actor Actor, submissionID int64) (Submission, error) {
	if err := requireActor(actor); err != nil {
		return Submission{}, err
	}
	if uc == nil || uc.repository == nil {
		return Submission{}, ErrorInternal("submission repository is not configured")
	}
	if submissionID <= 0 {
		return Submission{}, ErrorInvalidArgument("invalid submission id")
	}
	submission, err := uc.repository.FindByID(ctx, submissionID)
	if err != nil {
		return Submission{}, err
	}
	if submission.UserID != actor.ID && !actorIsAdmin(actor) {
		return Submission{}, ErrorSubmissionNotFound()
	}
	return submission, nil
}

func (uc *SubmissionUsecase) List(ctx context.Context, actor Actor, filter ListFilter) (SubmissionPage, error) {
	if err := requireActor(actor); err != nil {
		return SubmissionPage{}, err
	}
	if uc == nil || uc.repository == nil {
		return SubmissionPage{}, ErrorInternal("submission repository is not configured")
	}
	if filter.UserID < 0 || filter.ProblemID < 0 || filter.Page < 0 || filter.PageSize < 0 {
		return SubmissionPage{}, ErrorInvalidArgument("invalid submission filter")
	}
	if _, ok := submissionv1.SubmissionStatus_name[int32(filter.Status)]; !ok {
		return SubmissionPage{}, ErrorInvalidArgument("invalid submission status")
	}
	filter.Language = strings.ToLower(strings.TrimSpace(filter.Language))
	if filter.Language != "" && !SupportedLanguage(filter.Language) {
		return SubmissionPage{}, ErrorInvalidArgument("unsupported submission language")
	}
	if actorIsAdmin(actor) {
		return uc.repository.List(ctx, filter.Normalized())
	}
	if filter.UserID != 0 && filter.UserID != actor.ID {
		return SubmissionPage{}, ErrorPermissionDenied("cannot list another user's submissions")
	}
	filter.UserID = actor.ID
	return uc.repository.List(ctx, filter.Normalized())
}

func (uc *SubmissionUsecase) findCreateReplay(ctx context.Context, request IdempotencyRequest) (CreateSubmissionResult, bool, error) {
	record, found, err := uc.repository.FindIdempotency(ctx, request.ActorID, request.Operation, request.Key)
	if err != nil || !found {
		return CreateSubmissionResult{}, false, err
	}
	if record.RequestHash != request.RequestHash {
		return CreateSubmissionResult{}, true, ErrorIdempotencyConflict()
	}
	if len(record.Response) == 0 {
		return CreateSubmissionResult{}, true, ErrorIdempotencyInProgress()
	}
	var result CreateSubmissionResult
	if err := json.Unmarshal(record.Response, &result); err != nil || result.SubmissionID <= 0 || result.Status != submissionv1.SubmissionStatus_SUBMISSION_STATUS_QUEUED {
		return CreateSubmissionResult{}, true, ErrorInternal("stored idempotency response is invalid")
	}
	result.Replayed = true
	return result, true, nil
}

func requireActor(actor Actor) error {
	if actor.ID <= 0 {
		return ErrorUnauthenticated("trusted actor is missing")
	}
	return nil
}

func actorIsAdmin(actor Actor) bool {
	for _, role := range actor.Roles {
		if strings.EqualFold(strings.TrimSpace(role), "admin") {
			return true
		}
	}
	return false
}

func validateJudgeProfile(profile JudgeProfile, problemID int64) error {
	if profile.ProblemID != problemID || profile.TimeLimitMS <= 0 || profile.MemoryLimitKB <= 0 {
		return ErrorDependencyUnavailable("problem service returned an invalid judge profile")
	}
	if err := ValidateJudgeRevision(profile.JudgeRevision); err != nil {
		return ErrorProblemUnavailable("problem has no valid judge revision")
	}
	return nil
}

func createRequestHash(problemID int64, language string, source []byte) string {
	digest := sha256.New()
	var encodedID [8]byte
	binary.BigEndian.PutUint64(encodedID[:], uint64(problemID))
	_, _ = digest.Write(encodedID[:])
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write([]byte(language))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write(source)
	return hex.EncodeToString(digest.Sum(nil))
}

func (uc *SubmissionUsecase) currentTime() time.Time {
	if uc.now == nil {
		return time.Now().UTC()
	}
	return uc.now().UTC()
}

func (uc *SubmissionUsecase) eventID() string {
	if uc.newEventID == nil {
		return uuid.NewString()
	}
	return uc.newEventID()
}

func SupportedLanguage(language string) bool {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "cpp", "go", "python", "java":
		return true
	default:
		return false
	}
}

func LanguageExtension(language string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "cpp":
		return "cpp", true
	case "go":
		return "go", true
	case "python":
		return "py", true
	case "java":
		return "java", true
	default:
		return "", false
	}
}

func ValidateSubmissionForCreate(submission Submission) error {
	if submission.UserID <= 0 || submission.ProblemID <= 0 {
		return ErrorInvalidArgument("submission user and problem are required")
	}
	if !SupportedLanguage(submission.Language) {
		return ErrorInvalidArgument("unsupported submission language")
	}
	if len(submission.SourceObjectKey) == 0 || len(submission.SourceObjectKey) > MaxObjectKeyLength {
		return ErrorInvalidArgument("invalid source object key")
	}
	if !validSHA256(submission.SourceSHA256) {
		return ErrorInvalidArgument("invalid source sha256")
	}
	if submission.SourceSizeBytes <= 0 || submission.SourceSizeBytes > MaxSourceSizeBytes {
		return ErrorInvalidArgument("invalid source size")
	}
	if err := ValidateJudgeRevision(submission.JudgeRevision); err != nil {
		return err
	}
	if submission.JudgeDeadlineAt.IsZero() {
		return ErrorInvalidArgument("judge deadline is required")
	}
	if submission.RetryCount < 0 || submission.RetryCount > MaxRetryCount {
		return ErrorInvalidArgument("invalid retry count")
	}
	if submission.Status != submissionv1.SubmissionStatus_SUBMISSION_STATUS_UNSPECIFIED && submission.Status != submissionv1.SubmissionStatus_SUBMISSION_STATUS_QUEUED {
		return ErrorInvalidArgument("new submission must be queued")
	}
	if submission.Verdict != submissionv1.JudgeVerdict_JUDGE_VERDICT_UNSPECIFIED || submission.TimeMS != nil || submission.MemoryKB != nil || submission.RetryCount != 0 || submission.SystemErrorReason != "" || submission.JudgedAt != nil || submission.InvalidatedAt != nil {
		return ErrorInvalidArgument("new submission contains judge result state")
	}
	return nil
}

func ValidateJudgeRevision(revision string) error {
	if len(revision) != 26 {
		return ErrorInvalidArgument("invalid judge revision")
	}
	if _, err := ulid.ParseStrict(revision); err != nil {
		return ErrorInvalidArgument("invalid judge revision")
	}
	return nil
}

func ValidateIdempotency(request IdempotencyRequest, operation string, now time.Time) error {
	if request.ActorID <= 0 || request.Operation != operation {
		return ErrorInvalidArgument("invalid idempotency identity or operation")
	}
	parsed, err := uuid.Parse(request.Key)
	if err != nil || parsed.String() != strings.ToLower(request.Key) {
		return ErrorInvalidArgument("invalid idempotency key")
	}
	if !validSHA256(request.RequestHash) {
		return ErrorInvalidArgument("invalid idempotency request hash")
	}
	if !request.ExpiresAt.After(now) {
		return ErrorInvalidArgument("idempotency expiry must be in the future")
	}
	return nil
}

func ValidateEventID(value string) error {
	parsed, err := uuid.Parse(value)
	if err != nil || parsed.String() != strings.ToLower(value) {
		return ErrorInvalidArgument("invalid outbox event id")
	}
	return nil
}

func validSHA256(value string) bool {
	if len(value) != sha256HexLength || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256ByteLength
}

const (
	sha256ByteLength = 32
	sha256HexLength  = sha256ByteLength * 2
)

package mq

import (
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const (
	EventVersion1 int32 = 1

	EventTypeJudgeTask      = "judge.task"
	EventTypeJudgeCompleted = "judge.completed"
	EventTypeJudgeFailed    = "judge.failed"

	RoutingJudgeTaskCPP    = "judge.task.cpp"
	RoutingJudgeTaskGo     = "judge.task.go"
	RoutingJudgeTaskPython = "judge.task.python"
	RoutingJudgeTaskJava   = "judge.task.java"
	RoutingJudgeCompleted  = EventTypeJudgeCompleted
	RoutingJudgeFailed     = EventTypeJudgeFailed
)

// JudgeFailureCode is the stable, machine-readable classification for a
// judge failure. Message is reserved for human diagnostics and must not be
// used by retry policy decisions.
type JudgeFailureCode string

const (
	FailureSourceUnavailable   JudgeFailureCode = "SOURCE_UNAVAILABLE"
	FailureSourceCorrupted     JudgeFailureCode = "SOURCE_CORRUPTED"
	FailureRevisionUnavailable JudgeFailureCode = "REVISION_UNAVAILABLE"
	FailureRevisionCorrupted   JudgeFailureCode = "REVISION_CORRUPTED"
	FailureSandboxUnavailable  JudgeFailureCode = "SANDBOX_UNAVAILABLE"
	FailureSandboxInternal     JudgeFailureCode = "SANDBOX_INTERNAL"
	FailureTaskExpired         JudgeFailureCode = "TASK_EXPIRED"
	FailureWorkerInternal      JudgeFailureCode = "WORKER_INTERNAL"
)

type JudgeTask struct {
	SubmissionID    int64     `json:"submission_id"`
	ProblemID       int64     `json:"problem_id"`
	Language        string    `json:"language"`
	JudgeRevision   string    `json:"judge_revision"`
	SourceObjectKey string    `json:"source_object_key"`
	SourceSHA256    string    `json:"source_sha256"`
	SourceSizeBytes int64     `json:"source_size_bytes"`
	JudgeDeadlineAt time.Time `json:"judge_deadline_at"`
	Attempt         int32     `json:"attempt"`
}

type JudgeCaseResult struct {
	CaseNo   int32  `json:"case_no"`
	Verdict  string `json:"verdict"`
	TimeMS   int32  `json:"time_ms"`
	MemoryKB int32  `json:"memory_kb"`
	Message  string `json:"message,omitempty"`
}

type JudgeCompleted struct {
	SubmissionID  int64             `json:"submission_id"`
	JudgeRevision string            `json:"judge_revision"`
	Verdict       string            `json:"verdict"`
	TimeMS        int32             `json:"time_ms"`
	MemoryKB      int32             `json:"memory_kb"`
	CaseResults   []JudgeCaseResult `json:"case_results"`
}

type JudgeFailed struct {
	SubmissionID  int64            `json:"submission_id"`
	JudgeRevision string           `json:"judge_revision"`
	Code          JudgeFailureCode `json:"code,omitempty"`
	Message       string           `json:"message,omitempty"`
	Retryable     bool             `json:"retryable"`
	// Reason is retained for wire compatibility with pre-v1 failure events.
	// New producers must set Code and Message; retry policy must never parse it.
	Reason string `json:"reason,omitempty"`
}

func JudgeTaskRoutingKey(language string) (string, error) {
	language = strings.ToLower(strings.TrimSpace(language))
	switch language {
	case "cpp":
		return RoutingJudgeTaskCPP, nil
	case "go":
		return RoutingJudgeTaskGo, nil
	case "python":
		return RoutingJudgeTaskPython, nil
	case "java":
		return RoutingJudgeTaskJava, nil
	default:
		return "", fmt.Errorf("unsupported judge language %q", language)
	}
}

func JudgeTaskRoutingKeys() []string {
	return []string{RoutingJudgeTaskCPP, RoutingJudgeTaskGo, RoutingJudgeTaskPython, RoutingJudgeTaskJava}
}

func SupportedLanguage(language string) bool {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "cpp", "go", "python", "java":
		return true
	default:
		return false
	}
}

func (t JudgeTask) Validate() error {
	if t.SubmissionID <= 0 || t.ProblemID <= 0 || !SupportedLanguage(t.Language) || len(t.JudgeRevision) != 26 {
		return fmt.Errorf("invalid judge task identity")
	}
	if t.Attempt < 0 {
		return fmt.Errorf("invalid judge task attempt")
	}
	if t.SourceObjectKey == "" || t.SourceSizeBytes <= 0 || !validSHA256(t.SourceSHA256) {
		return fmt.Errorf("invalid judge task source metadata")
	}
	if t.JudgeDeadlineAt.IsZero() {
		return fmt.Errorf("invalid judge task deadline")
	}
	return nil
}

func (e JudgeCompleted) Validate() error {
	if e.SubmissionID <= 0 || len(e.JudgeRevision) != 26 || !validVerdict(e.Verdict) || e.TimeMS < 0 || e.MemoryKB < 0 {
		return fmt.Errorf("invalid judge completed event")
	}
	seen := make(map[int32]struct{}, len(e.CaseResults))
	for _, result := range e.CaseResults {
		if result.CaseNo <= 0 || !validVerdict(result.Verdict) || result.TimeMS < 0 || result.MemoryKB < 0 {
			return fmt.Errorf("invalid judge case result")
		}
		if _, ok := seen[result.CaseNo]; ok {
			return fmt.Errorf("duplicate judge case result %d", result.CaseNo)
		}
		seen[result.CaseNo] = struct{}{}
	}
	return nil
}

func (e JudgeFailed) Validate() error {
	if e.SubmissionID <= 0 || len(e.JudgeRevision) != 26 {
		return fmt.Errorf("invalid judge failed event")
	}
	// Accept legacy Reason-only events while older services are being rolled
	// forward. New events should always carry Code; an empty code is rejected
	// once no compatibility reason is available.
	if strings.TrimSpace(string(e.Code)) == "" && strings.TrimSpace(e.Reason) == "" {
		return fmt.Errorf("invalid judge failed event")
	}
	if len(e.Code) > 64 || len(e.Message) > 1024 || len(e.Reason) > 128 {
		return fmt.Errorf("invalid judge failed event")
	}
	if e.Code != "" && !validFailureCode(e.Code) {
		return fmt.Errorf("invalid judge failure code")
	}
	return nil
}

func validFailureCode(value JudgeFailureCode) bool {
	value = JudgeFailureCode(strings.TrimSpace(string(value)))
	if value == "" {
		return false
	}
	for _, ch := range value {
		if (ch < 'A' || ch > 'Z') && (ch < '0' || ch > '9') && ch != '_' {
			return false
		}
	}
	return true
}

func validSHA256(value string) bool {
	digest, err := hex.DecodeString(value)
	return err == nil && len(digest) == 32 && value == strings.ToLower(value)
}

func validVerdict(value string) bool {
	value = strings.ToUpper(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "JUDGE_VERDICT_")
	switch value {
	case "AC", "WA", "TLE", "MLE", "RE", "CE":
		return true
	default:
		return false
	}
}

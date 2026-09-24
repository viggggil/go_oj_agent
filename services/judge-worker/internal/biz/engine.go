package biz

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/viggggil/go_oj_agent/pkg/mq"
)

type Engine struct {
	loader     TaskLoader
	runner     LanguageRunner
	comparator Comparator
	now        func() time.Time
}

func NewEngine(loader TaskLoader, runner LanguageRunner, comparator Comparator) *Engine {
	return &Engine{loader: loader, runner: runner, comparator: comparator, now: func() time.Time { return time.Now().UTC() }}
}

func (e *Engine) Execute(ctx context.Context, task mq.JudgeTask) Outcome {
	if err := task.Validate(); err != nil {
		return failed(task, "INVALID_TASK", false)
	}
	if e == nil || e.loader == nil || e.runner == nil || e.comparator == nil {
		return failed(task, "WORKER_NOT_CONFIGURED", true)
	}
	if !e.now().Before(task.JudgeDeadlineAt) {
		return failed(task, "JUDGE_DEADLINE_EXCEEDED", false)
	}
	loaded, err := e.loader.Load(ctx, task)
	if err != nil {
		return outcomeFromError(task, err)
	}
	if err = validateLoadedTask(task, loaded); err != nil {
		return outcomeFromError(task, err)
	}

	compile, err := e.runner.Compile(ctx, loaded.Source)
	if err != nil {
		return outcomeFromError(task, err)
	}
	if compile.Verdict == VerdictCE {
		return completed(task, VerdictCE, 0, 0, nil)
	}
	if compile.Verdict != VerdictAC || compile.Artifact.ID == "" {
		return failed(task, "INVALID_COMPILE_RESULT", true)
	}

	result := e.executeCases(ctx, task, compile.Artifact, loaded)
	if err = e.runner.DeleteArtifact(context.WithoutCancel(ctx), compile.Artifact); err != nil && result.Failed == nil {
		return outcomeFromError(task, NewSystemError("ARTIFACT_CLEANUP_FAILED", true, err))
	}
	return result
}

func (e *Engine) executeCases(ctx context.Context, task mq.JudgeTask, artifact Artifact, loaded LoadedTask) Outcome {
	limits := ResourceLimits{
		TimeLimit:   time.Duration(loaded.Manifest.TimeLimitMS) * time.Millisecond,
		MemoryBytes: uint64(loaded.Manifest.MemoryLimitKB) * 1024,
	}
	caseResults := make([]mq.JudgeCaseResult, 0, len(loaded.Cases))
	overall, maxTimeMS, maxMemoryKB := VerdictAC, int32(0), int32(0)
	for _, testcase := range loaded.Cases {
		if !e.now().Before(task.JudgeDeadlineAt) {
			return failed(task, "JUDGE_DEADLINE_EXCEEDED", false)
		}
		run, err := e.runner.Run(ctx, artifact, testcase.Input, limits)
		if err != nil {
			return outcomeFromError(task, err)
		}
		verdict := run.Verdict
		if verdict == VerdictAC && !e.comparator.Equal(testcase.Expected, run.Output) {
			verdict = VerdictWA
		}
		if !validExecutionVerdict(verdict) {
			return failed(task, "INVALID_RUN_RESULT", true)
		}
		timeMS := durationMilliseconds(run.Time)
		memoryKB := bytesKilobytes(run.Memory)
		if timeMS > maxTimeMS {
			maxTimeMS = timeMS
		}
		if memoryKB > maxMemoryKB {
			maxMemoryKB = memoryKB
		}
		caseResults = append(caseResults, mq.JudgeCaseResult{
			CaseNo: testcase.CaseNo, Verdict: verdict, TimeMS: timeMS, MemoryKB: memoryKB, Message: run.Message,
		})
		if verdict != VerdictAC {
			overall = verdict
			break
		}
	}
	return completed(task, overall, maxTimeMS, maxMemoryKB, caseResults)
}

func validateLoadedTask(task mq.JudgeTask, loaded LoadedTask) error {
	if loaded.Task != task {
		return NewSystemError("TASK_SNAPSHOT_MISMATCH", false, nil)
	}
	if err := loaded.Manifest.Validate(); err != nil {
		return NewSystemError("INVALID_JUDGE_MANIFEST", false, err)
	}
	if loaded.Manifest.ProblemID != task.ProblemID || loaded.Manifest.JudgeRevision != task.JudgeRevision {
		return NewSystemError("JUDGE_REVISION_MISMATCH", false, nil)
	}
	if len(loaded.Source) == 0 || len(loaded.Cases) != len(loaded.Manifest.Testcases) {
		return NewSystemError("INCOMPLETE_TASK_INPUT", false, nil)
	}
	sort.Slice(loaded.Cases, func(i, j int) bool { return loaded.Cases[i].CaseNo < loaded.Cases[j].CaseNo })
	for i := range loaded.Cases {
		if loaded.Cases[i].CaseNo != loaded.Manifest.Testcases[i].CaseNo {
			return NewSystemError("TESTCASE_SNAPSHOT_MISMATCH", false, nil)
		}
	}
	return nil
}

func outcomeFromError(task mq.JudgeTask, err error) Outcome {
	reason, retryable := ClassifySystemError(err)
	return failed(task, reason, retryable)
}

func failed(task mq.JudgeTask, reason string, retryable bool) Outcome {
	return Outcome{Failed: &mq.JudgeFailed{
		SubmissionID: task.SubmissionID, JudgeRevision: task.JudgeRevision, Reason: reason, Retryable: retryable,
	}}
}

func completed(task mq.JudgeTask, verdict string, timeMS, memoryKB int32, cases []mq.JudgeCaseResult) Outcome {
	return Outcome{Completed: &mq.JudgeCompleted{
		SubmissionID: task.SubmissionID, JudgeRevision: task.JudgeRevision, Verdict: verdict,
		TimeMS: timeMS, MemoryKB: memoryKB, CaseResults: cases,
	}}
}

func validExecutionVerdict(verdict string) bool {
	switch verdict {
	case VerdictAC, VerdictWA, VerdictTLE, VerdictMLE, VerdictRE:
		return true
	default:
		return false
	}
}

func durationMilliseconds(value time.Duration) int32 {
	if value <= 0 {
		return 0
	}
	value = (value + time.Millisecond - 1) / time.Millisecond
	if value > time.Duration(^uint32(0)>>1) {
		return int32(^uint32(0) >> 1)
	}
	return int32(value)
}

func bytesKilobytes(value uint64) int32 {
	if value == 0 {
		return 0
	}
	value = (value + 1023) / 1024
	if value > uint64(^uint32(0)>>1) {
		return int32(^uint32(0) >> 1)
	}
	return int32(value)
}

func (o Outcome) Validate() error {
	if (o.Completed == nil) == (o.Failed == nil) {
		return fmt.Errorf("outcome must contain exactly one result")
	}
	if o.Completed != nil {
		return o.Completed.Validate()
	}
	return o.Failed.Validate()
}

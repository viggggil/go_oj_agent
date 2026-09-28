package biz

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	judgecontract "github.com/viggggil/go_oj_agent/pkg/judge"
	"github.com/viggggil/go_oj_agent/pkg/mq"
)

type fakeLoader struct {
	loaded LoadedTask
	err    error
}

func (f *fakeLoader) Load(context.Context, mq.JudgeTask) (LoadedTask, error) { return f.loaded, f.err }

type fakeRunner struct {
	compile    CompileResult
	compileErr error
	runs       []ExecutionResult
	runErr     error
	deleted    int
	runIndex   int
}

func (f *fakeRunner) Compile(context.Context, []byte) (CompileResult, error) {
	return f.compile, f.compileErr
}
func (f *fakeRunner) Run(context.Context, Artifact, []byte, ResourceLimits) (ExecutionResult, error) {
	if f.runErr != nil {
		return ExecutionResult{}, f.runErr
	}
	result := f.runs[f.runIndex]
	f.runIndex++
	return result, nil
}
func (f *fakeRunner) DeleteArtifact(context.Context, Artifact) error { f.deleted++; return nil }

type exactComparator struct{}

func (exactComparator) Equal(expected, actual []byte) bool { return string(expected) == string(actual) }

func TestEngineVerdicts(t *testing.T) {
	tests := []struct {
		name       string
		compile    CompileResult
		runs       []ExecutionResult
		want       string
		wantCases  int
		wantDelete int
	}{
		{"accepted", CompileResult{Artifact: Artifact{ID: "binary"}, Verdict: VerdictAC}, []ExecutionResult{{Verdict: VerdictAC, Output: []byte("3\n"), Time: 3 * time.Millisecond, Memory: 2049}}, VerdictAC, 1, 1},
		{"wrong answer", CompileResult{Artifact: Artifact{ID: "binary"}, Verdict: VerdictAC}, []ExecutionResult{{Verdict: VerdictAC, Output: []byte("4\n")}}, VerdictWA, 1, 1},
		{"compile error", CompileResult{Verdict: VerdictCE, Message: "compile failed"}, nil, VerdictCE, 0, 0},
		{"time limit", CompileResult{Artifact: Artifact{ID: "binary"}, Verdict: VerdictAC}, []ExecutionResult{{Verdict: VerdictTLE, Time: time.Second}}, VerdictTLE, 1, 1},
		{"memory limit", CompileResult{Artifact: Artifact{ID: "binary"}, Verdict: VerdictAC}, []ExecutionResult{{Verdict: VerdictMLE, Memory: 65537}}, VerdictMLE, 1, 1},
		{"runtime error", CompileResult{Artifact: Artifact{ID: "binary"}, Verdict: VerdictAC}, []ExecutionResult{{Verdict: VerdictRE, Message: "NON_ZERO_EXIT"}}, VerdictRE, 1, 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			task, loaded := validLoadedTask()
			runner := &fakeRunner{compile: test.compile, runs: test.runs}
			engine := NewEngine(&fakeLoader{loaded: loaded}, runner, exactComparator{})
			engine.now = func() time.Time { return time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC) }
			outcome := engine.Execute(t.Context(), task)
			if outcome.Failed != nil || outcome.Completed == nil || outcome.Completed.Verdict != test.want {
				t.Fatalf("outcome = %+v", outcome)
			}
			if len(outcome.Completed.CaseResults) != test.wantCases || runner.deleted != test.wantDelete {
				t.Fatalf("cases=%d deleted=%d", len(outcome.Completed.CaseResults), runner.deleted)
			}
			if err := outcome.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestEngineSystemFailures(t *testing.T) {
	task, loaded := validLoadedTask()
	tests := []struct {
		name      string
		loader    *fakeLoader
		runner    *fakeRunner
		now       time.Time
		reason    string
		retryable bool
	}{
		{"deadline", &fakeLoader{loaded: loaded}, &fakeRunner{}, task.JudgeDeadlineAt, "JUDGE_DEADLINE_EXCEEDED", false},
		{"storage", &fakeLoader{err: NewSystemError("SOURCE_STORE_UNAVAILABLE", true, errors.New("offline"))}, &fakeRunner{}, time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC), "SOURCE_STORE_UNAVAILABLE", true},
		{"sandbox", &fakeLoader{loaded: loaded}, &fakeRunner{compileErr: NewSystemError("SANDBOX_UNAVAILABLE", true, errors.New("offline"))}, time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC), "SANDBOX_UNAVAILABLE", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			engine := NewEngine(test.loader, test.runner, exactComparator{})
			engine.now = func() time.Time { return test.now }
			outcome := engine.Execute(t.Context(), task)
			if outcome.Failed == nil || outcome.Completed != nil || outcome.Failed.Reason != test.reason || outcome.Failed.Retryable != test.retryable {
				t.Fatalf("outcome = %+v", outcome)
			}
		})
	}
}

func TestEngineStopsAfterFirstFailedCase(t *testing.T) {
	task, loaded := validLoadedTask()
	loaded.Manifest.Testcases = append(loaded.Manifest.Testcases, judgecontract.Testcase{
		CaseNo: 2,
		Input:  judgecontract.Object{ObjectKey: "problem-7/judge-revisions/" + task.JudgeRevision + "/testcases/2.in", SHA256: strings.Repeat("c", 64), SizeBytes: 2},
		Output: judgecontract.Object{ObjectKey: "problem-7/judge-revisions/" + task.JudgeRevision + "/testcases/2.out", SHA256: strings.Repeat("d", 64), SizeBytes: 2},
	})
	loaded.Cases = append(loaded.Cases, LoadedCase{CaseNo: 2, Input: []byte("2\n"), Expected: []byte("2\n")})
	runner := &fakeRunner{
		compile: CompileResult{Artifact: Artifact{ID: "binary"}, Verdict: VerdictAC},
		runs: []ExecutionResult{
			{Verdict: VerdictAC, Output: []byte("wrong\n")},
			{Verdict: VerdictAC, Output: []byte("2\n")},
		},
	}
	engine := NewEngine(&fakeLoader{loaded: loaded}, runner, exactComparator{})
	engine.now = func() time.Time { return time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC) }
	outcome := engine.Execute(t.Context(), task)
	if outcome.Completed == nil || outcome.Completed.Verdict != VerdictWA || len(outcome.Completed.CaseResults) != 1 || runner.runIndex != 1 {
		t.Fatalf("outcome=%+v runs=%d", outcome, runner.runIndex)
	}
}

func TestEngineCleansArtifactAfterRunFailure(t *testing.T) {
	task, loaded := validLoadedTask()
	runner := &fakeRunner{
		compile: CompileResult{Artifact: Artifact{ID: "binary"}, Verdict: VerdictAC},
		runErr:  NewSystemError("SANDBOX_UNAVAILABLE", true, errors.New("offline")),
	}
	engine := NewEngine(&fakeLoader{loaded: loaded}, runner, exactComparator{})
	engine.now = func() time.Time { return time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC) }
	outcome := engine.Execute(t.Context(), task)
	if outcome.Failed == nil || runner.deleted != 1 {
		t.Fatalf("outcome=%+v deleted=%d", outcome, runner.deleted)
	}
}

func validLoadedTask() (mq.JudgeTask, LoadedTask) {
	revision := "01K5C6Y7N8P9Q0R1S2T3V4W5X6"
	task := mq.JudgeTask{
		SubmissionID: 9, ProblemID: 7, Language: "go", JudgeRevision: revision,
		SourceObjectKey: "sources/id/source.go", SourceSHA256: strings.Repeat("a", 64), SourceSizeBytes: 13,
		JudgeDeadlineAt: time.Date(2026, 9, 24, 0, 5, 0, 0, time.UTC),
	}
	manifest := judgecontract.Manifest{
		ManifestVersion: judgecontract.ManifestVersion, ProblemID: 7, JudgeRevision: revision,
		TimeLimitMS: 1000, MemoryLimitKB: 65536,
		Testcases: []judgecontract.Testcase{{
			CaseNo: 1,
			Input:  judgecontract.Object{ObjectKey: "problem-7/judge-revisions/" + revision + "/testcases/1.in", SHA256: strings.Repeat("b", 64), SizeBytes: 4},
			Output: judgecontract.Object{ObjectKey: "problem-7/judge-revisions/" + revision + "/testcases/1.out", SHA256: strings.Repeat("c", 64), SizeBytes: 2},
		}},
	}
	return task, LoadedTask{Task: task, Source: []byte("package main"), Manifest: manifest, Cases: []LoadedCase{{CaseNo: 1, Input: []byte("1 2\n"), Expected: []byte("3\n")}}}
}

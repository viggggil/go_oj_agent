package language

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/biz"
	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/sandbox"
)

type fakeSandbox struct {
	results  []sandbox.Result
	err      error
	requests []sandbox.Request
	deleted  []string
}

func (f *fakeSandbox) Execute(_ context.Context, request sandbox.Request) (sandbox.Result, error) {
	f.requests = append(f.requests, request)
	if f.err != nil {
		return sandbox.Result{}, f.err
	}
	result := f.results[0]
	f.results = f.results[1:]
	return result, nil
}

func (f *fakeSandbox) DeleteFile(_ context.Context, fileID string) error {
	f.deleted = append(f.deleted, fileID)
	return f.err
}

func TestGoRunnerCompile(t *testing.T) {
	executor := &fakeSandbox{results: []sandbox.Result{{Status: sandbox.StatusAccepted, FileIDs: map[string]string{"main": "artifact-1"}}}}
	runner := NewGoRunner(executor, GoConfig{CompileTimeLimit: 3 * time.Second, CompileMemoryBytes: 128 << 20, ProcessLimit: 8, OutputLimitBytes: 4096})
	result, err := runner.Compile(t.Context(), []byte("package main\nfunc main() {}"))
	if err != nil || result.Verdict != biz.VerdictAC || result.Artifact.ID != "artifact-1" {
		t.Fatalf("Compile() = %+v, %v", result, err)
	}
	request := executor.requests[0]
	if !reflect.DeepEqual(request.Args, []string{"/usr/local/go/bin/go", "build", "-trimpath", "-o", "main", "main.go"}) {
		t.Fatalf("compile args = %v", request.Args)
	}
	if request.CPULimit != 3*time.Second || request.ClockLimit != 6*time.Second || request.MemoryLimit != 128<<20 || request.OutputLimit != 4096 {
		t.Fatalf("compile limits = %+v", request)
	}
	if string(request.CopyIn["main.go"].Content) == "" || !reflect.DeepEqual(request.CacheOut, []string{"main"}) {
		t.Fatalf("compile files = %+v", request)
	}
}

func TestGoRunnerCompileErrorIsUserVerdict(t *testing.T) {
	executor := &fakeSandbox{results: []sandbox.Result{{Status: sandbox.StatusNonZeroExit, Files: map[string][]byte{"stderr": []byte("syntax error")}}}}
	result, err := NewGoRunner(executor, GoConfig{}).Compile(t.Context(), []byte("invalid"))
	if err != nil || result.Verdict != biz.VerdictCE || result.Message != "syntax error" {
		t.Fatalf("Compile() = %+v, %v", result, err)
	}
}

func TestGoRunnerRunStatusMapping(t *testing.T) {
	tests := []struct {
		status  sandbox.Status
		verdict string
		message string
		system  bool
	}{
		{sandbox.StatusAccepted, biz.VerdictAC, "", false},
		{sandbox.StatusTimeLimitExceeded, biz.VerdictTLE, "TIME_LIMIT_EXCEEDED", false},
		{sandbox.StatusMemoryLimitExceeded, biz.VerdictMLE, "MEMORY_LIMIT_EXCEEDED", false},
		{sandbox.StatusOutputLimitExceeded, biz.VerdictRE, "OUTPUT_LIMIT_EXCEEDED", false},
		{sandbox.StatusNonZeroExit, biz.VerdictRE, "NON_ZERO_EXIT", false},
		{sandbox.StatusSignalled, biz.VerdictRE, "PROCESS_SIGNALLED", false},
		{sandbox.StatusDangerousSyscall, biz.VerdictRE, "DANGEROUS_SYSCALL", false},
		{sandbox.StatusInternalError, "", "SANDBOX_INTERNAL_ERROR", true},
	}
	for _, test := range tests {
		t.Run(test.message+test.verdict, func(t *testing.T) {
			executor := &fakeSandbox{results: []sandbox.Result{{Status: test.status, Time: 4 * time.Millisecond, Memory: 2048, Files: map[string][]byte{"stdout": []byte("ok\n")}}}}
			runner := NewGoRunner(executor, GoConfig{ProcessLimit: 4, OutputLimitBytes: 1024})
			result, err := runner.Run(t.Context(), biz.Artifact{ID: "artifact"}, []byte("input\n"), biz.ResourceLimits{TimeLimit: time.Second, MemoryBytes: 64 << 20})
			if test.system {
				if err == nil {
					t.Fatal("Run() expected system error")
				}
				reason, retryable := biz.ClassifySystemError(err)
				if reason != test.message || !retryable {
					t.Fatalf("system error = %q, %v", reason, retryable)
				}
				return
			}
			if err != nil || result.Verdict != test.verdict || result.Message != test.message {
				t.Fatalf("Run() = %+v, %v", result, err)
			}
			request := executor.requests[0]
			if request.ClockLimit != 2*time.Second || request.CopyIn["main"].CachedFileID != "artifact" || request.MemoryLimit != 64<<20 {
				t.Fatalf("run request = %+v", request)
			}
		})
	}
}

func TestGoRunnerSandboxFailureIsRetryable(t *testing.T) {
	runner := NewGoRunner(&fakeSandbox{err: errors.New("connection refused")}, GoConfig{})
	_, err := runner.Compile(t.Context(), []byte("package main"))
	reason, retryable := biz.ClassifySystemError(err)
	if reason != "SANDBOX_UNAVAILABLE" || !retryable {
		t.Fatalf("error = %q retryable=%v", reason, retryable)
	}
}

func TestGoRunnerDeleteArtifact(t *testing.T) {
	executor := &fakeSandbox{}
	if err := NewGoRunner(executor, GoConfig{}).DeleteArtifact(t.Context(), biz.Artifact{ID: "artifact"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(executor.deleted, []string{"artifact"}) {
		t.Fatalf("deleted = %v", executor.deleted)
	}
}

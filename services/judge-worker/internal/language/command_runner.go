package language

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/biz"
	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/sandbox"
)

// CommandRunner adapts a conventional compiler/interpreter command to the
// shared sandbox contract. All commands execute inside go-judge; the worker
// never invokes a toolchain on its own host.
type CommandRunner struct {
	sandbox                                                 sandbox.Executor
	language, compiler, sourceName, artifactName            string
	compileArgs                                             func(string) []string
	compileTime                                             time.Duration
	compileMemory, processLimit, compileOutput, outputLimit uint64
}

func NewCommandRunner(executor sandbox.Executor, language, compiler, sourceName, artifactName string, compileArgs func(string) []string) *CommandRunner {
	return &CommandRunner{sandbox: executor, language: language, compiler: compiler, sourceName: sourceName, artifactName: artifactName, compileArgs: compileArgs, compileTime: 30 * time.Second, compileMemory: 512 << 20, processLimit: 64, compileOutput: 64 << 20, outputLimit: 1 << 20}
}

func (r *CommandRunner) Compile(ctx context.Context, source []byte) (biz.CompileResult, error) {
	if r == nil || r.sandbox == nil || len(source) == 0 || r.compiler == "" || r.sourceName == "" {
		return biz.CompileResult{}, biz.NewSystemError("INVALID_SOURCE", false, nil)
	}
	args := []string{r.compiler}
	if r.compileArgs != nil {
		args = append(args, r.compileArgs(r.sourceName)...)
	}
	result, err := r.sandbox.Execute(ctx, sandbox.Request{
		RequestID: "compile-" + r.language, Args: args,
		Env:    []string{"PATH=/usr/local/go/bin:/usr/bin:/bin", "HOME=/tmp", "TMPDIR=/tmp", "GOMAXPROCS=2", "GOFLAGS=-p=2", "LANG=C.UTF-8"},
		CopyIn: map[string]sandbox.File{r.sourceName: {Content: source}}, CacheOut: []string{r.artifactName},
		CPULimit: r.compileTime, ClockLimit: clockLimit(r.compileTime), MemoryLimit: r.compileMemory, ProcessLimit: r.processLimit, OutputLimit: r.compileOutput,
	})
	if err != nil {
		return biz.CompileResult{}, biz.NewSystemError("SANDBOX_UNAVAILABLE", true, err)
	}
	if result.Status == sandbox.StatusAccepted {
		id := result.FileIDs[r.artifactName]
		if id == "" {
			return biz.CompileResult{}, biz.NewSystemError("COMPILE_ARTIFACT_MISSING", true, nil)
		}
		return biz.CompileResult{Artifact: biz.Artifact{ID: id}, Verdict: biz.VerdictAC}, nil
	}
	if result.Status == sandbox.StatusNonZeroExit {
		message := commandDiagnostic(result)
		slog.Warn("language compilation rejected", "language", r.language, "status", result.Status, "exit_code", result.ExitStatus, "stderr", message)
		return biz.CompileResult{Verdict: biz.VerdictCE, Message: message}, nil
	}
	return biz.CompileResult{}, commandCompileSystemError(result, r.language)
}

func (r *CommandRunner) Run(ctx context.Context, artifact biz.Artifact, input []byte, limits biz.ResourceLimits) (biz.ExecutionResult, error) {
	if r == nil || r.sandbox == nil || artifact.ID == "" || limits.TimeLimit <= 0 || limits.MemoryBytes == 0 {
		return biz.ExecutionResult{}, biz.NewSystemError("INVALID_RUN_REQUEST", false, nil)
	}
	args := r.runArgs()
	result, err := r.sandbox.Execute(ctx, sandbox.Request{RequestID: "run-" + r.language, Args: args, Env: []string{"PATH=/usr/local/go/bin:/usr/bin:/bin", "HOME=/tmp", "TMPDIR=/tmp", "LANG=C.UTF-8"}, Stdin: input, CopyIn: map[string]sandbox.File{r.artifactName: {CachedFileID: artifact.ID}}, CPULimit: limits.TimeLimit, ClockLimit: clockLimit(limits.TimeLimit), MemoryLimit: limits.MemoryBytes, ProcessLimit: r.processLimit, OutputLimit: r.outputLimit})
	if err != nil {
		return biz.ExecutionResult{}, biz.NewSystemError("SANDBOX_UNAVAILABLE", true, err)
	}
	verdict, message, systemFailure := commandMapRunStatus(result.Status)
	if systemFailure {
		slog.Error("language execution sandbox failure", "language", r.language, "status", result.Status, "exit_code", result.ExitStatus, "cpu_time", result.Time, "clock_time", result.RunTime, "memory", result.Memory, "stderr", commandDiagnostic(result), "error", result.Error)
		return biz.ExecutionResult{}, biz.NewSystemError(message, true, nil)
	}
	return biz.ExecutionResult{Verdict: verdict, Time: result.Time, Memory: result.Memory, Output: append([]byte(nil), result.Files["stdout"]...), Message: message}, nil
}

func (r *CommandRunner) runArgs() []string {
	switch r.language {
	case "c", "cpp":
		return []string{"./main"}
	case "java":
		return []string{"java", "Main"}
	default:
		return []string{"python3", r.artifactName}
	}
}

func (r *CommandRunner) DeleteArtifact(ctx context.Context, artifact biz.Artifact) error {
	if r == nil || r.sandbox == nil || artifact.ID == "" {
		return fmt.Errorf("invalid compile artifact")
	}
	return r.sandbox.DeleteFile(ctx, artifact.ID)
}

func commandMapRunStatus(status sandbox.Status) (string, string, bool) {
	switch status {
	case sandbox.StatusAccepted:
		return biz.VerdictAC, "", false
	case sandbox.StatusTimeLimitExceeded:
		return biz.VerdictTLE, "TIME_LIMIT_EXCEEDED", false
	case sandbox.StatusMemoryLimitExceeded:
		return biz.VerdictMLE, "MEMORY_LIMIT_EXCEEDED", false
	case sandbox.StatusOutputLimitExceeded:
		return biz.VerdictRE, "OUTPUT_LIMIT_EXCEEDED", false
	case sandbox.StatusNonZeroExit:
		return biz.VerdictRE, "NON_ZERO_EXIT", false
	case sandbox.StatusSignalled:
		return biz.VerdictRE, "PROCESS_SIGNALLED", false
	case sandbox.StatusDangerousSyscall:
		return biz.VerdictRE, "DANGEROUS_SYSCALL", false
	default:
		return "", "SANDBOX_INTERNAL_ERROR", true
	}
}

func commandCompileSystemError(result sandbox.Result, language string) error {
	reason := "SANDBOX_COMPILE_FAILURE"
	switch result.Status {
	case sandbox.StatusTimeLimitExceeded:
		reason = "SANDBOX_COMPILE_TIMEOUT"
	case sandbox.StatusMemoryLimitExceeded:
		reason = "SANDBOX_COMPILE_MEMORY_LIMIT"
	case sandbox.StatusOutputLimitExceeded:
		reason = "SANDBOX_COMPILE_OUTPUT_LIMIT"
	case sandbox.StatusInternalError, sandbox.StatusInvalid:
		reason = "SANDBOX_INTERNAL_ERROR"
	}
	diagnostic := commandDiagnostic(result)
	slog.Error("language compilation sandbox failure", "language", language, "status", result.Status, "exit_code", result.ExitStatus, "cpu_time", result.Time, "clock_time", result.RunTime, "memory", result.Memory, "stderr", diagnostic, "error", result.Error)
	return biz.NewSystemError(reason, true, fmt.Errorf("%s", diagnostic))
}

func commandDiagnostic(result sandbox.Result) string {
	value := strings.TrimSpace(string(result.Files["stderr"]))
	if value == "" {
		value = strings.TrimSpace(result.Error)
	}
	if len(value) > 4096 {
		value = value[:4096]
	}
	return value
}

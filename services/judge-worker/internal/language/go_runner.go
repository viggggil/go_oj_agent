package language

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/biz"
	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/sandbox"
)

type GoConfig struct {
	CompilerPath       string
	CompileTimeLimit   time.Duration
	CompileMemoryBytes uint64
	ProcessLimit       uint64
	OutputLimitBytes   uint64
}

func (c GoConfig) normalized() GoConfig {
	if c.CompilerPath == "" {
		c.CompilerPath = "/usr/local/go/bin/go"
	}
	if c.CompileTimeLimit <= 0 {
		c.CompileTimeLimit = 10 * time.Second
	}
	if c.CompileMemoryBytes == 0 {
		c.CompileMemoryBytes = 512 << 20
	}
	if c.ProcessLimit == 0 {
		c.ProcessLimit = 64
	}
	if c.OutputLimitBytes == 0 {
		c.OutputLimitBytes = 1 << 20
	}
	return c
}

type GoRunner struct {
	sandbox sandbox.Executor
	config  GoConfig
}

func NewGoRunner(executor sandbox.Executor, config GoConfig) *GoRunner {
	return &GoRunner{sandbox: executor, config: config.normalized()}
}

func (r *GoRunner) Compile(ctx context.Context, source []byte) (biz.CompileResult, error) {
	if r == nil || r.sandbox == nil || len(source) == 0 {
		return biz.CompileResult{}, biz.NewSystemError("INVALID_SOURCE", false, nil)
	}
	config := r.config.normalized()
	result, err := r.sandbox.Execute(ctx, sandbox.Request{
		RequestID: "compile", Args: []string{config.CompilerPath, "build", "-trimpath", "-o", "main", "main.go"},
		Env:    []string{"PATH=/usr/local/go/bin:/usr/bin:/bin", "HOME=/tmp", "GOCACHE=/tmp/go-cache", "GOMODCACHE=/tmp/go-mod-cache", "CGO_ENABLED=0"},
		CopyIn: map[string]sandbox.File{"main.go": {Content: source}}, CacheOut: []string{"main"},
		CPULimit: config.CompileTimeLimit, ClockLimit: clockLimit(config.CompileTimeLimit),
		MemoryLimit: config.CompileMemoryBytes, ProcessLimit: config.ProcessLimit, OutputLimit: config.OutputLimitBytes,
	})
	if err != nil {
		return biz.CompileResult{}, biz.NewSystemError("SANDBOX_UNAVAILABLE", true, err)
	}
	if result.Status == sandbox.StatusInternalError || result.Status == sandbox.StatusInvalid || result.Status == sandbox.StatusFileError {
		return biz.CompileResult{}, biz.NewSystemError("SANDBOX_INTERNAL_ERROR", true, nil)
	}
	if result.Status != sandbox.StatusAccepted {
		return biz.CompileResult{Verdict: biz.VerdictCE, Message: boundedDiagnostic(result)}, nil
	}
	fileID := result.FileIDs["main"]
	if fileID == "" {
		return biz.CompileResult{}, biz.NewSystemError("COMPILE_ARTIFACT_MISSING", true, nil)
	}
	return biz.CompileResult{Artifact: biz.Artifact{ID: fileID}, Verdict: biz.VerdictAC}, nil
}

func (r *GoRunner) Run(ctx context.Context, artifact biz.Artifact, input []byte, limits biz.ResourceLimits) (biz.ExecutionResult, error) {
	if r == nil || r.sandbox == nil || artifact.ID == "" || limits.TimeLimit <= 0 || limits.MemoryBytes == 0 {
		return biz.ExecutionResult{}, biz.NewSystemError("INVALID_RUN_REQUEST", false, nil)
	}
	config := r.config.normalized()
	result, err := r.sandbox.Execute(ctx, sandbox.Request{
		RequestID: "run", Args: []string{"./main"}, Stdin: input,
		CopyIn:   map[string]sandbox.File{"main": {CachedFileID: artifact.ID}},
		CPULimit: limits.TimeLimit, ClockLimit: clockLimit(limits.TimeLimit), MemoryLimit: limits.MemoryBytes,
		ProcessLimit: config.ProcessLimit, OutputLimit: config.OutputLimitBytes,
	})
	if err != nil {
		return biz.ExecutionResult{}, biz.NewSystemError("SANDBOX_UNAVAILABLE", true, err)
	}
	verdict, message, systemFailure := mapRunStatus(result.Status)
	if systemFailure {
		return biz.ExecutionResult{}, biz.NewSystemError(message, true, nil)
	}
	return biz.ExecutionResult{
		Verdict: verdict, Time: result.Time, Memory: result.Memory,
		Output: append([]byte(nil), result.Files["stdout"]...), Message: message,
	}, nil
}

func (r *GoRunner) DeleteArtifact(ctx context.Context, artifact biz.Artifact) error {
	if r == nil || r.sandbox == nil || artifact.ID == "" {
		return fmt.Errorf("invalid compile artifact")
	}
	return r.sandbox.DeleteFile(ctx, artifact.ID)
}

func clockLimit(cpu time.Duration) time.Duration {
	doubled := cpu * 2
	buffered := cpu + 500*time.Millisecond
	if doubled > buffered {
		return doubled
	}
	return buffered
}

func mapRunStatus(status sandbox.Status) (verdict, message string, systemFailure bool) {
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
	case sandbox.StatusFileError, sandbox.StatusInternalError, sandbox.StatusInvalid:
		return "", "SANDBOX_INTERNAL_ERROR", true
	default:
		return "", "SANDBOX_UNKNOWN_STATUS", true
	}
}

func boundedDiagnostic(result sandbox.Result) string {
	value := strings.TrimSpace(string(result.Files["stderr"]))
	if value == "" {
		value = strings.TrimSpace(result.Error)
	}
	if len(value) > 4096 {
		value = value[:4096]
	}
	return value
}

package biz

import (
	"context"
	"time"

	judgecontract "github.com/viggggil/go_oj_agent/pkg/judge"
	"github.com/viggggil/go_oj_agent/pkg/mq"
)

const (
	VerdictAC  = "AC"
	VerdictWA  = "WA"
	VerdictTLE = "TLE"
	VerdictMLE = "MLE"
	VerdictRE  = "RE"
	VerdictCE  = "CE"
)

type LoadedCase struct {
	CaseNo   int32
	Input    []byte
	Expected []byte
}

type LoadedTask struct {
	Task     mq.JudgeTask
	Source   []byte
	Manifest judgecontract.Manifest
	Cases    []LoadedCase
}

type Artifact struct {
	ID string
}

type ResourceLimits struct {
	TimeLimit   time.Duration
	MemoryBytes uint64
}

type ExecutionResult struct {
	Verdict string
	Time    time.Duration
	Memory  uint64
	Output  []byte
	Message string
}

type CompileResult struct {
	Artifact Artifact
	Verdict  string
	Message  string
}

type TaskLoader interface {
	Load(context.Context, mq.JudgeTask) (LoadedTask, error)
}

type LanguageRunner interface {
	Compile(context.Context, []byte) (CompileResult, error)
	Run(context.Context, Artifact, []byte, ResourceLimits) (ExecutionResult, error)
	DeleteArtifact(context.Context, Artifact) error
}

type Comparator interface {
	Equal(expected, actual []byte) bool
}

type Outcome struct {
	Completed *mq.JudgeCompleted
	Failed    *mq.JudgeFailed
}

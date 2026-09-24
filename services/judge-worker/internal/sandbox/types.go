package sandbox

import (
	"context"
	"time"
)

type Status int

const (
	StatusInvalid Status = iota
	StatusAccepted
	StatusMemoryLimitExceeded
	StatusTimeLimitExceeded
	StatusOutputLimitExceeded
	StatusFileError
	StatusNonZeroExit
	StatusSignalled
	StatusDangerousSyscall
	StatusInternalError
)

type File struct {
	Content      []byte
	CachedFileID string
}

type Request struct {
	RequestID    string
	Args         []string
	Env          []string
	Stdin        []byte
	CopyIn       map[string]File
	CacheOut     []string
	CPULimit     time.Duration
	ClockLimit   time.Duration
	MemoryLimit  uint64
	ProcessLimit uint64
	OutputLimit  uint64
}

type Result struct {
	Status     Status
	ExitStatus int32
	Time       time.Duration
	RunTime    time.Duration
	Memory     uint64
	Files      map[string][]byte
	FileIDs    map[string]string
	Error      string
}

type Executor interface {
	Execute(context.Context, Request) (Result, error)
	DeleteFile(context.Context, string) error
}

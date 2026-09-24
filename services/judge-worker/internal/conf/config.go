package conf

import (
	"fmt"
	"strings"
	"time"
)

type Bootstrap struct {
	Service  Service  `json:"service" yaml:"service"`
	Sandbox  Sandbox  `json:"sandbox" yaml:"sandbox"`
	Language Language `json:"language" yaml:"language"`
}

type Service struct {
	Name string `json:"name" yaml:"name"`
}

type Sandbox struct {
	GoJudge GoJudge `json:"go_judge" yaml:"go_judge"`
}

type GoJudge struct {
	Endpoint string `json:"endpoint" yaml:"endpoint"`
	Token    string `json:"token" yaml:"token"`
	Timeout  string `json:"timeout" yaml:"timeout"`
}

type Language struct {
	Go Go `json:"go" yaml:"go"`
}

type Go struct {
	CompilerPath       string `json:"compiler_path" yaml:"compiler_path"`
	CompileTimeLimit   string `json:"compile_time_limit" yaml:"compile_time_limit"`
	CompileMemoryBytes uint64 `json:"compile_memory_bytes" yaml:"compile_memory_bytes"`
	ProcessLimit       uint64 `json:"process_limit" yaml:"process_limit"`
	OutputLimitBytes   uint64 `json:"output_limit_bytes" yaml:"output_limit_bytes"`
}

func (c *Bootstrap) Validate() error {
	if c == nil || strings.TrimSpace(c.Service.Name) == "" {
		return fmt.Errorf("judge-worker service name is required")
	}
	if strings.TrimSpace(c.Sandbox.GoJudge.Endpoint) == "" || strings.TrimSpace(c.Sandbox.GoJudge.Token) == "" {
		return fmt.Errorf("go-judge endpoint and token are required")
	}
	if _, err := ParseDuration(c.Sandbox.GoJudge.Timeout, 30*time.Second); err != nil {
		return fmt.Errorf("invalid go-judge timeout: %w", err)
	}
	if _, err := ParseDuration(c.Language.Go.CompileTimeLimit, 10*time.Second); err != nil {
		return fmt.Errorf("invalid Go compile time limit: %w", err)
	}
	return nil
}

func ParseDuration(value string, fallback time.Duration) (time.Duration, error) {
	if strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("duration must be positive")
	}
	return parsed, nil
}

package conf

import (
	"fmt"
	"strings"
	"time"
)

type Bootstrap struct {
	Service   Service   `json:"service" yaml:"service"`
	Sandbox   Sandbox   `json:"sandbox" yaml:"sandbox"`
	Language  Language  `json:"language" yaml:"language"`
	Storage   Storage   `json:"storage" yaml:"storage"`
	Worker    Worker    `json:"worker" yaml:"worker"`
	Messaging Messaging `json:"messaging" yaml:"messaging"`
}

type Worker struct {
	Concurrency     int    `json:"concurrency" yaml:"concurrency"`
	TaskTimeout     string `json:"task_timeout" yaml:"task_timeout"`
	ShutdownTimeout string `json:"shutdown_timeout" yaml:"shutdown_timeout"`
	Retry           Retry  `json:"retry" yaml:"retry"`
}
type Retry struct {
	MaxRetries int    `json:"max_retries" yaml:"max_retries"`
	Delay      string `json:"delay" yaml:"delay"`
}
type Messaging struct {
	RabbitMQ RabbitMQ `json:"rabbitmq" yaml:"rabbitmq"`
}
type RabbitMQ struct {
	URL            string `json:"url" yaml:"url"`
	Exchange       string `json:"exchange" yaml:"exchange"`
	Queue          string `json:"queue" yaml:"queue"`
	RetryQueue     string `json:"retry_queue" yaml:"retry_queue"`
	DLQ            string `json:"dlq" yaml:"dlq"`
	ConfirmTimeout string `json:"confirm_timeout" yaml:"confirm_timeout"`
}

type Storage struct {
	MinIO MinIO `json:"minio" yaml:"minio"`
}
type MinIO struct {
	Endpoint      string `json:"endpoint" yaml:"endpoint"`
	AccessKey     string `json:"access_key" yaml:"access_key"`
	SecretKey     string `json:"secret_key" yaml:"secret_key"`
	SourceBucket  string `json:"source_bucket" yaml:"source_bucket"`
	ProblemBucket string `json:"problem_bucket" yaml:"problem_bucket"`
	UseSSL        bool   `json:"use_ssl" yaml:"use_ssl"`
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
	if strings.TrimSpace(c.Storage.MinIO.Endpoint) == "" || c.Storage.MinIO.AccessKey == "" || c.Storage.MinIO.SecretKey == "" || strings.TrimSpace(c.Storage.MinIO.SourceBucket) == "" || strings.TrimSpace(c.Storage.MinIO.ProblemBucket) == "" {
		return fmt.Errorf("judge-worker minio endpoint, credentials and buckets are required")
	}
	if c.Worker.Concurrency == 0 {
		c.Worker.Concurrency = 4
	}
	if c.Worker.Concurrency < 1 {
		return fmt.Errorf("worker concurrency must be positive")
	}
	if _, err := ParseDuration(c.Worker.TaskTimeout, 60*time.Second); err != nil {
		return fmt.Errorf("invalid worker task timeout: %w", err)
	}
	if _, err := ParseDuration(c.Worker.ShutdownTimeout, 30*time.Second); err != nil {
		return fmt.Errorf("invalid worker shutdown timeout: %w", err)
	}
	if c.Worker.Retry.MaxRetries < 0 {
		return fmt.Errorf("worker retry max retries cannot be negative")
	}
	if _, err := ParseDuration(c.Worker.Retry.Delay, 5*time.Second); err != nil {
		return fmt.Errorf("invalid worker retry delay: %w", err)
	}
	if c.Messaging.RabbitMQ.URL != "" {
		if strings.TrimSpace(c.Messaging.RabbitMQ.Exchange) == "" {
			return fmt.Errorf("rabbitmq exchange is required")
		}
		if c.Messaging.RabbitMQ.Queue == "" {
			c.Messaging.RabbitMQ.Queue = "judge.task.go"
		}
		if c.Messaging.RabbitMQ.RetryQueue == "" {
			c.Messaging.RabbitMQ.RetryQueue = "judge.retry.go"
		}
		if c.Messaging.RabbitMQ.DLQ == "" {
			c.Messaging.RabbitMQ.DLQ = "judge.dlq"
		}
		if _, err := ParseDuration(c.Messaging.RabbitMQ.ConfirmTimeout, 5*time.Second); err != nil {
			return fmt.Errorf("invalid rabbitmq confirm timeout: %w", err)
		}
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

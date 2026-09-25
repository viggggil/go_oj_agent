package message

import (
	"fmt"
	"time"

	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/biz"
	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/conf"
)

// NewConsumerFromConfig builds the Go-task consumer using the worker's single
// concurrency and timeout settings. RabbitMQ topology is opened by Start.
func NewConsumerFromConfig(config *conf.Bootstrap, engine Engine, reporter Reporter) (*Consumer, error) {
	if config == nil {
		return nil, fmt.Errorf("judge-worker configuration is required")
	}
	timeout, err := conf.ParseDuration(config.Worker.TaskTimeout, 60*time.Second)
	if err != nil {
		return nil, err
	}
	if config.Worker.Concurrency < 1 {
		return nil, fmt.Errorf("worker concurrency must be positive")
	}
	rabbit := config.Messaging.RabbitMQ
	if rabbit.URL == "" || rabbit.Exchange == "" {
		return nil, fmt.Errorf("rabbitmq url and exchange are required")
	}
	return &Consumer{URL: rabbit.URL, Exchange: rabbit.Exchange, Queue: rabbit.Queue, Concurrency: config.Worker.Concurrency, TaskTimeout: timeout, Engine: engine, Reporter: reporter}, nil
}

func NewReporterFromConfig(config *conf.Bootstrap) (*RabbitReporter, error) {
	if config == nil {
		return nil, fmt.Errorf("judge-worker configuration is required")
	}
	rabbit := config.Messaging.RabbitMQ
	if rabbit.URL == "" || rabbit.Exchange == "" {
		return nil, fmt.Errorf("rabbitmq url and exchange are required")
	}
	timeout, err := conf.ParseDuration(rabbit.ConfirmTimeout, 5*time.Second)
	if err != nil {
		return nil, err
	}
	return NewRabbitReporter(rabbit.URL, rabbit.Exchange, timeout), nil
}

var _ Engine = (*biz.Engine)(nil)

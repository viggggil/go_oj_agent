package conf

import "testing"

func TestValidateConfig(t *testing.T) {
	valid := &Bootstrap{Service: &ServiceProto{Name: "contest-service"}, Server: &ServerProto{Grpc: &GRPCProto{Address: ":9005"}}, Data: &DataProto{MysqlDsn: "test"}, Messaging: &MessagingProto{Url: "amqp://localhost", Exchange: "oj.events", Queue: "contest.submission-judged", DeadLetterQueue: "contest.results.dlq", Prefetch: 16}}
	if err := ValidateConfig(valid); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	if err := ValidateConfig(&Bootstrap{}); err == nil {
		t.Fatal("empty config accepted")
	}
	valid.Messaging.Prefetch = 257
	if err := ValidateConfig(valid); err == nil {
		t.Fatal("unbounded prefetch accepted")
	}
	valid.Messaging = nil
	if err := ValidateConfig(valid); err == nil {
		t.Fatal("missing messaging accepted")
	}
}

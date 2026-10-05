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

func TestValidateLeaderboardCache(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cache *LeaderboardCacheProto
		valid bool
	}{
		{"absent", nil, true},
		{"disabled", &LeaderboardCacheProto{}, true},
		{"valid", &LeaderboardCacheProto{Enabled: true, Addresses: []string{"localhost:6379"}, Namespace: "oj:contest", Timeout: "2s"}, true},
		{"cluster", &LeaderboardCacheProto{Enabled: true, Addresses: []string{"redis1:6379", "redis2:6379"}, Namespace: "oj:contest", Timeout: "2s"}, true},
		{"no address", &LeaderboardCacheProto{Enabled: true, Namespace: "oj:contest", Timeout: "2s"}, false},
		{"blank address", &LeaderboardCacheProto{Enabled: true, Addresses: []string{" "}, Namespace: "oj:contest", Timeout: "2s"}, false},
		{"negative db", &LeaderboardCacheProto{Enabled: true, Addresses: []string{"localhost:6379"}, Db: -1, Namespace: "oj:contest", Timeout: "2s"}, false},
		{"cluster nonzero db", &LeaderboardCacheProto{Enabled: true, Addresses: []string{"redis1:6379", "redis2:6379"}, Db: 1, Namespace: "oj:contest", Timeout: "2s"}, false},
		{"hash tag injection", &LeaderboardCacheProto{Enabled: true, Addresses: []string{"localhost:6379"}, Namespace: "oj:{other}", Timeout: "2s"}, false},
		{"zero timeout", &LeaderboardCacheProto{Enabled: true, Addresses: []string{"localhost:6379"}, Namespace: "oj:contest", Timeout: "0s"}, false},
		{"unbounded timeout", &LeaderboardCacheProto{Enabled: true, Addresses: []string{"localhost:6379"}, Namespace: "oj:contest", Timeout: "4s"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateLeaderboardCache(tc.cache); (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
}

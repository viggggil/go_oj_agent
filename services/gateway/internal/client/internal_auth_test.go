package client

import (
	"testing"

	"github.com/viggggil/go_oj_agent/services/gateway/internal/conf"
)

func TestInternalAudience(t *testing.T) {
	tests := []struct {
		name     string
		client   *conf.ClientProto
		fallback string
		want     string
	}{
		{name: "target service name", client: &conf.ClientProto{Name: "judge-service"}, fallback: "problem-service", want: "judge-service"},
		{name: "legacy fallback", client: &conf.ClientProto{}, fallback: "problem-service", want: "problem-service"},
		{name: "nil client", fallback: "problem-service", want: "problem-service"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := internalAudience(test.client, test.fallback); got != test.want {
				t.Fatalf("internalAudience() = %q, want %q", got, test.want)
			}
		})
	}
}

package servicediscovery

import "testing"

func TestNewDisabledDoesNotRequireConsulAddress(t *testing.T) {
	discovery, err := New(Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if discovery != nil {
		t.Fatalf("New() discovery = %v, want nil", discovery)
	}
}

func TestNewEnabledRequiresAddress(t *testing.T) {
	if _, err := New(Config{Enabled: true}); err == nil {
		t.Fatal("New() expected missing address error")
	}
}

func TestTarget(t *testing.T) {
	tests := []struct {
		name     string
		enabled  bool
		service  string
		fallback string
		want     string
	}{
		{name: "discovery target", enabled: true, service: "user-service", fallback: "127.0.0.1:9001", want: "discovery:///user-service"},
		{name: "trim service name", enabled: true, service: " /problem-service ", want: "discovery:///problem-service"},
		{name: "fallback when disabled", service: "user-service", fallback: "127.0.0.1:9001", want: "127.0.0.1:9001"},
		{name: "fallback when name missing", enabled: true, fallback: "127.0.0.1:9001", want: "127.0.0.1:9001"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Target(test.enabled, test.service, test.fallback); got != test.want {
				t.Fatalf("Target() = %q, want %q", got, test.want)
			}
		})
	}
}

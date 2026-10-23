package client

import (
	"net/http"
	"testing"
)

func TestAgentToolDescriptionOperationIsRestricted(t *testing.T) {
	path := "/api/v1/admin/agent/tools/get_problem"
	if _, err := AgentOperation(http.MethodPut, path); err != nil {
		t.Fatal("tool description write rejected:", err)
	}
	for _, test := range []struct{ method, path string }{
		{http.MethodGet, path},
		{http.MethodPost, path},
		{http.MethodPut, "/api/v1/admin/agent/tools/Bad_Name"},
		{http.MethodPut, path + "/extra"},
	} {
		if _, err := AgentOperation(test.method, test.path); err == nil {
			t.Fatalf("accepted %s %s", test.method, test.path)
		}
	}
}

func TestAgentObservabilityOperationsAreReadOnly(t *testing.T) {
	for _, path := range []string{"/api/v1/admin/agent/observability/overview", "/api/v1/admin/agent/observability/runs"} {
		if _, err := AgentOperation(http.MethodGet, path); err != nil {
			t.Fatalf("GET %s rejected: %v", path, err)
		}
		if _, err := AgentOperation(http.MethodPost, path); err == nil {
			t.Fatalf("POST %s accepted", path)
		}
	}
}

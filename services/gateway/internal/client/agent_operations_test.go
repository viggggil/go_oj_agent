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

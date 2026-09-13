package biz

import (
	"testing"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
)

func TestRequireAdmin(t *testing.T) {
	if err := requireAdmin(&commonv1.RequestContext{UserId: 1, Roles: []string{" ADMIN "}}); err != nil {
		t.Fatalf("admin context rejected: %v", err)
	}
	if err := requireAdmin(&commonv1.RequestContext{UserId: 1, Roles: []string{"user"}}); !problemv1.IsProblemErrorReasonPermissionDenied(err) {
		t.Fatalf("expected permission denied, got %v", err)
	}
	if err := requireAdmin(nil); !problemv1.IsProblemErrorReasonInvalidArgument(err) {
		t.Fatalf("expected invalid argument, got %v", err)
	}
}

func TestNormalizeTags(t *testing.T) {
	got := normalizeTags([]string{" DP ", "dp", "", "Graph"})
	want := []string{"dp", "graph"}
	if len(got) != len(want) {
		t.Fatalf("normalizeTags() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("normalizeTags() = %v, want %v", got, want)
		}
	}
}

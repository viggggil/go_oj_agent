package version

import "testing"

func TestModulePath(t *testing.T) {
	const want = "github.com/viggggil/go_oj_agent"

	if got := ModulePath(); got != want {
		t.Fatalf("ModulePath() = %q, want %q", got, want)
	}
}

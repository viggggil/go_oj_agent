package contestv1

import (
	"testing"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
)

func TestContestRequestsValidate(t *testing.T) {
	if err := (&GetContestRequest{ContestId: 1}).Validate(); err != nil {
		t.Fatalf("valid get request rejected: %v", err)
	}
	if err := (&GetContestRequest{}).Validate(); err == nil {
		t.Fatal("missing contest id accepted")
	}
	if err := (&ListContestsRequest{Page: &commonv1.PageRequest{Page: 1, PageSize: 20}}).Validate(); err != nil {
		t.Fatalf("valid list request rejected: %v", err)
	}
}

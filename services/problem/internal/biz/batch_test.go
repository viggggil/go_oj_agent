package biz

import (
	"context"
	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	"testing"
)

type batchRepository struct {
	fakeProblemRepository
	includeArchived bool
	calls           int
}

func (r *batchRepository) BatchGet(_ context.Context, _ []int64, archived bool) ([]Problem, error) {
	r.includeArchived = archived
	r.calls++
	return []Problem{{ID: 1, Title: "A"}}, nil
}
func TestBatchGetUsesActorPermissionsAndValidatesIDs(t *testing.T) {
	repo := &batchRepository{}
	uc := NewProblemUsecase(repo)
	for _, role := range []string{"user", "admin"} {
		actor := &commonv1.RequestContext{UserId: 42, Roles: []string{role}}
		if _, err := uc.BatchGet(context.Background(), actor, []int64{1}); err != nil {
			t.Fatal(err)
		}
		if repo.includeArchived != (role == "admin") {
			t.Fatalf("archive visibility for %s", role)
		}
	}
	for _, ids := range [][]int64{nil, {0}, {1, 1}} {
		if _, err := uc.BatchGet(context.Background(), adminContext(), ids); err == nil {
			t.Fatalf("accepted %v", ids)
		}
	}
	if repo.calls != 2 {
		t.Fatalf("invalid IDs reached repository")
	}
}

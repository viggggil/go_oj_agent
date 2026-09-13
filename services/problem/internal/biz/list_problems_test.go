package biz

import (
	"context"
	"testing"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
)

func TestListProblemsNormalizesPaginationAndVisibility(t *testing.T) {
	repo := &listRepository{}
	uc := NewProblemUsecase(repo)
	page, err := uc.List(context.Background(), &commonv1.RequestContext{UserId: 1, Roles: []string{"user"}}, 0, 500)
	if err != nil {
		t.Fatal(err)
	}
	if page.Page != 1 || page.PageSize != 100 || repo.includeArchived {
		t.Fatalf("page = %+v, includeArchived = %v", page, repo.includeArchived)
	}
	_, err = uc.List(context.Background(), &commonv1.RequestContext{UserId: 2, Roles: []string{"admin"}}, 2, 10)
	if err != nil || !repo.includeArchived {
		t.Fatalf("admin list error = %v, includeArchived = %v", err, repo.includeArchived)
	}
}

type listRepository struct{ includeArchived bool }

func (*listRepository) Create(context.Context, Problem, []string) (Problem, error) {
	return Problem{}, nil
}
func (*listRepository) FindByID(context.Context, int64) (Problem, error) { return Problem{}, nil }
func (r *listRepository) List(_ context.Context, _, _ int32, includeArchived bool) ([]Problem, int64, error) {
	r.includeArchived = includeArchived
	return []Problem{{ID: 1}}, 1, nil
}
func (*listRepository) Update(context.Context, Problem, []string) (Problem, error) {
	return Problem{}, nil
}

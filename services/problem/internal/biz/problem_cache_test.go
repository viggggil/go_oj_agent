package biz

import (
	"context"
	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"testing"
)

type fakeProblemCache struct {
	problem       Problem
	found         bool
	sets, deletes int
}

func (c *fakeProblemCache) Get(context.Context, int64) (Problem, bool, error) {
	return c.problem, c.found, nil
}
func (c *fakeProblemCache) Set(_ context.Context, p Problem) error {
	c.problem = p
	c.sets++
	return nil
}
func (c *fakeProblemCache) Delete(context.Context, int64) error { c.deletes++; return nil }
func TestGetProblemUsesCache(t *testing.T) {
	cache := &fakeProblemCache{problem: Problem{ID: 2, Status: problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL}, found: true}
	repo := &fakeProblemRepository{err: context.Canceled}
	got, err := NewProblemUsecaseWithDependencies(repo, nil, nil, nil, cache).Get(context.Background(), &commonv1.RequestContext{UserId: 1, Roles: []string{"user"}}, 2)
	if err != nil || got.ID != 2 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}
func TestUpdateProblemInvalidatesCache(t *testing.T) {
	cache := &fakeProblemCache{}
	repo := &fakeProblemRepository{created: Problem{ID: 2, Status: problemv1.ProblemStatus_PROBLEM_STATUS_NORMAL}}
	_, err := NewProblemUsecaseWithDependencies(repo, nil, nil, nil, cache).Update(context.Background(), adminContext(), 2, Problem{Title: "A", Slug: "a", Description: "S", Difficulty: problemv1.ProblemDifficulty_PROBLEM_DIFFICULTY_EASY, TimeLimitMs: 1, MemoryLimitKb: 1}, nil)
	if err != nil || cache.deletes != 1 {
		t.Fatalf("err=%v deletes=%d", err, cache.deletes)
	}
}

package biz

import (
	"context"
	"errors"
	"testing"
	"time"
)

type titleCatalog struct {
	ids []int64
	err error
}

func (c *titleCatalog) Titles(_ context.Context, ids []int64) (map[int64]string, error) {
	c.ids = append([]int64(nil), ids...)
	return map[int64]string{7: "Two Sum"}, c.err
}
func TestDetailsRestoresMembershipAndLoadsTitles(t *testing.T) {
	repo := &contestSubmissionRepo{fakeContestRepo: fakeContestRepo{contest: Contest{ID: 20, StartAt: time.Now().Add(time.Hour), EndAt: time.Now().Add(2 * time.Hour), Problems: []ContestProblem{{ProblemID: 7}, {ProblemID: 8}}}}}
	catalog := &titleCatalog{}
	uc := NewContestUsecaseWithClients(repo, nil, catalog)
	for _, joined := range []bool{true, false} {
		repo.participant = joined
		got, err := uc.GetDetails(context.Background(), &commonRequestUser42, 20)
		if err != nil || got.Joined != joined || got.Problems[0].Title != "Two Sum" || got.Problems[1].Title != "" {
			t.Fatalf("details=%+v err=%v", got, err)
		}
		if len(catalog.ids) != 2 {
			t.Fatalf("batch IDs=%v", catalog.ids)
		}
	}
	catalog.err = errors.New("problem unavailable")
	if _, err := uc.GetDetails(context.Background(), &commonRequestUser42, 20); !errors.Is(err, catalog.err) {
		t.Fatalf("error=%v", err)
	}
}

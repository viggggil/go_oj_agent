package data

import (
	"context"
	"reflect"
	"testing"
	"time"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	contestv1 "github.com/viggggil/go_oj_agent/api/contest/v1"
	"github.com/viggggil/go_oj_agent/pkg/internalauth"
	"github.com/viggggil/go_oj_agent/services/contest/internal/biz"
	"github.com/viggggil/go_oj_agent/services/contest/internal/service"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestKnowledgeAccessUsesAllContestsAndDatabaseClock(t *testing.T) {
	db := testContestDatabase(t)
	now := time.Now().UTC()
	for index, item := range []struct {
		state      string
		start, end time.Time
		problem    int64
	}{
		{"draft", now.Add(time.Hour), now.Add(2 * time.Hour), 10},
		{"running", now.Add(-time.Hour), now.Add(time.Hour), 11},
		{"running", now.Add(-2 * time.Hour), now.Add(-time.Hour), 12},
		{"archived", now.Add(time.Hour), now.Add(2 * time.Hour), 13},
		{"running", now.Add(-2 * time.Hour), now.Add(-time.Hour), 10},
	} {
		id := index + 1
		if _, err := db.Exec(`INSERT INTO contests(id,title,status,start_at,end_at,created_by,created_at,updated_at) VALUES (?,'knowledge',?,?,?,1,?,?)`, id, item.state, item.start, item.end, now, now); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO contest_problems VALUES (?,?,1,100)`, id, item.problem); err != nil {
			t.Fatal(err)
		}
	}
	repo := &Repository{db: db}
	uc := biz.NewContestUsecaseWithRepository(repo)
	for _, roles := range [][]string{{"user"}, {"admin"}} {
		got, err := uc.CheckKnowledgeAccess(context.Background(), &commonv1.RequestContext{UserId: 7, Roles: roles}, []int64{10, 11, 12, 13, 14})
		if err != nil || !reflect.DeepEqual(got, []int64{12, 13, 14}) {
			t.Fatalf("roles=%v got=%v err=%v", roles, got, err)
		}
	}
	s := service.NewContestService(uc)
	if _, err := s.CheckKnowledgeAccess(context.Background(), &contestv1.CheckKnowledgeAccessRequest{ProblemIds: []int64{14}}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("unauthenticated err=%v", err)
	}
	ctx := internalauth.WithPrincipal(context.Background(), internalauth.Principal{ActorID: 7})
	for _, ids := range [][]int64{nil, {0}, {10, 10}} {
		if _, err := s.CheckKnowledgeAccess(ctx, &contestv1.CheckKnowledgeAccessRequest{ProblemIds: ids}); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("ids=%v err=%v", ids, err)
		}
	}
	response, err := s.CheckKnowledgeAccess(ctx, &contestv1.CheckKnowledgeAccessRequest{ProblemIds: []int64{11, 14}})
	if err != nil || !reflect.DeepEqual(response.GetAllowedProblemIds(), []int64{14}) {
		t.Fatalf("response=%v err=%v", response, err)
	}
}

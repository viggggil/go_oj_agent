package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	contestv1 "github.com/viggggil/go_oj_agent/api/contest/v1"
	submissionv1 "github.com/viggggil/go_oj_agent/api/submission/v1"
	"github.com/viggggil/go_oj_agent/pkg/internalauth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestContestLeaderboardFlow(t *testing.T) {
	baseURL, contestDSN, endpoint, keyFile := os.Getenv("AUTH_INTEGRATION_BASE_URL"), os.Getenv("CONTEST_TEST_MYSQL_DSN"), os.Getenv("CONTEST_TEST_GRPC_ENDPOINT"), os.Getenv("JUDGE_TEST_GATEWAY_PRIVATE_KEY_FILE")
	if baseURL == "" || contestDSN == "" || endpoint == "" || keyFile == "" {
		t.Skip("set contest e2e settings")
	}
	api := apiClient{baseURL: baseURL, client: &http.Client{Timeout: 10 * time.Second}}
	userDB := openIntegrationDB(t, os.Getenv("PROBLEM_TEST_USER_MYSQL_DSN"))
	contestDB := openIntegrationDB(t, contestDSN)
	submissionDB := openIntegrationDB(t, os.Getenv("SUBMISSION_TEST_MYSQL_DSN"))
	adminID, token := registerJudgeIntegrationUser(t, api, userDB, "contestadmin", true)
	problemID := createJudgeIntegrationProblem(t, api, token)
	// 保持 Gateway 用户 token 有效，长轮询排行榜通过内部认证 gRPC 执行。
	userID, userToken := registerJudgeIntegrationUser(t, api, userDB, "contestuser", false)
	key, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := internalauth.NewSigner(key, "gateway-internal-2026-09", "go-oj-gateway", "contest-service", "gateway-service", 30*time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(internalauth.UnaryClientInterceptor(signer, func(ctx context.Context) internalauth.Actor {
		p, _ := internalauth.PrincipalFromContext(ctx)
		return internalauth.Actor{ID: p.ActorID, Roles: p.ActorRoles, RequestID: p.RequestID, TraceID: p.TraceID}
	})))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := contestv1.NewContestServiceClient(conn)
	future := time.Now().UTC().Add(time.Hour).Truncate(time.Millisecond)
	created, err := client.CreateContest(judgeActorContext(t.Context(), adminID, "admin"), &contestv1.CreateContestRequest{Contest: &contestv1.ContestUpdate{Title: "Contest leaderboard E2E", StartAt: timestamppb.New(future), EndAt: timestamppb.New(future.Add(2 * time.Hour)), Problems: []*contestv1.ContestProblem{{ProblemId: problemID, SortOrder: 1}}}})
	if err != nil {
		t.Fatal(err)
	}
	id := created.GetContest().GetId()
	response := api.request(t, http.MethodPost, fmt.Sprintf("/api/v1/contests/%d/join", id), map[string]any{}, userToken)
	assertStatus(t, response, http.StatusOK)
	response.Body.Close()
	// 测试时钟推进通过夹具修改本地比赛起止，避免等待一小时。
	start := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Millisecond)
	if _, err := contestDB.Exec(`UPDATE contests SET start_at=?,end_at=? WHERE id=?`, start, start.Add(time.Hour), id); err != nil {
		t.Fatal(err)
	}
	submit := func(source string) int64 {
		response := api.request(t, http.MethodPost, fmt.Sprintf("/api/v1/contests/%d/problems/%d/submissions", id, problemID), map[string]any{"language": "go", "source_code": source, "idempotency_key": uuid.NewString()}, userToken)
		assertStatus(t, response, http.StatusOK)
		var result struct {
			SubmissionID json.Number `json:"submission_id"`
		}
		decodeJSON(t, response.Body, &result)
		response.Body.Close()
		submissionID, err := result.SubmissionID.Int64()
		if err != nil {
			t.Fatal(err)
		}
		return submissionID
	}
	waID := submit("package main\nimport \"fmt\"\nfunc main(){fmt.Println(0)}\n")
	acID := submit("package main\nimport \"fmt\"\nfunc main(){var a,b int;fmt.Scan(&a,&b);fmt.Println(a+b)}\n")
	request := &contestv1.GetLeaderboardRequest{ContestId: id, Page: &commonv1.PageRequest{Page: 1, PageSize: 20}}
	ctx := judgeActorContext(t.Context(), userID, "user")
	var entry *contestv1.LeaderboardEntry
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		reply, err := client.GetLeaderboard(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		if len(reply.Items) == 1 && reply.Items[0].SolvedCount == 1 && reply.Items[0].Problems[0].WrongAttempts == 1 {
			entry = reply.Items[0]
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if entry == nil {
		t.Fatal("WA -> AC leaderboard did not converge")
	}
	var submitted time.Time
	if err := submissionDB.QueryRow(`SELECT created_at FROM submissions WHERE id=?`, acID).Scan(&submitted); err != nil {
		t.Fatal(err)
	}
	expected := int64(submitted.Sub(start)/time.Second) + 1200
	if entry.UserId != userID || entry.PenaltySeconds != expected || !entry.Problems[0].AcceptedAt.AsTime().Equal(submitted) {
		t.Fatalf("leaderboard=%v expected penalty=%d", entry, expected)
	}
	for submissionID, verdict := range map[int64]string{waID: "WA", acID: "AC"} {
		var payloadContest int64
		var payloadVerdict string
		var status string
		if err := submissionDB.QueryRow(`SELECT CAST(JSON_UNQUOTE(JSON_EXTRACT(o.payload,'$.contest_id')) AS SIGNED),JSON_UNQUOTE(JSON_EXTRACT(o.payload,'$.verdict')),s.status FROM outbox_events o JOIN submissions s ON s.id=o.aggregate_id WHERE o.aggregate_id=? AND o.event_type='submission.judged'`, submissionID).Scan(&payloadContest, &payloadVerdict, &status); err != nil {
			t.Fatal(err)
		}
		if payloadContest != id || payloadVerdict != verdict || status != "DONE" {
			t.Fatalf("contest event=(%d,%s,%s)", payloadContest, payloadVerdict, status)
		}
	}
	// 比赛结束后的真实重判：旧 AC 作废，新 ID 仍保持原提交时间与罚时。
	if _, err := contestDB.Exec(`UPDATE contests SET end_at=? WHERE id=?`, time.Now().UTC().Add(-time.Second), id); err != nil {
		t.Fatal(err)
	}
	judgeClient := newJudgeIntegrationClient(t, os.Getenv("JUDGE_TEST_GRPC_ENDPOINT"), keyFile)
	rejudged, err := judgeClient.RejudgeSubmission(judgeActorContext(t.Context(), adminID, "admin"), &submissionv1.RejudgeSubmissionRequest{SubmissionId: acID, IdempotencyKey: uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	if rejudged.GetSubmission().GetId() == acID || !rejudged.GetSubmission().GetCreatedAt().AsTime().Equal(submitted) {
		t.Fatalf("replacement=%v", rejudged)
	}
	deadline = time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		var invalidated bool
		var replacementCount int
		oldErr := contestDB.QueryRow(`SELECT invalidated FROM contest_submission_results WHERE submission_id=?`, acID).Scan(&invalidated)
		newErr := contestDB.QueryRow(`SELECT COUNT(*) FROM contest_submission_results WHERE submission_id=? AND verdict='AC'`, rejudged.GetSubmission().GetId()).Scan(&replacementCount)
		if oldErr == nil && newErr == nil && invalidated && replacementCount == 1 {
			reply, err := client.GetLeaderboard(ctx, request)
			if err != nil || len(reply.Items) != 1 || reply.Items[0].SolvedCount != 1 || reply.Items[0].PenaltySeconds != expected {
				t.Fatalf("rejudged leaderboard=%v err=%v", reply, err)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("post-contest rejudge did not converge")
}

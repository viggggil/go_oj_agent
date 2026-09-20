package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	submissionv1 "github.com/viggggil/go_oj_agent/api/submission/v1"
	"github.com/viggggil/go_oj_agent/pkg/internalauth"
)

func TestJudgeSubmissionFlow(t *testing.T) {
	baseURL := os.Getenv("AUTH_INTEGRATION_BASE_URL")
	userMySQLDSN := os.Getenv("PROBLEM_TEST_USER_MYSQL_DSN")
	problemMySQLDSN := os.Getenv("PROBLEM_TEST_MYSQL_DSN")
	submissionMySQLDSN := os.Getenv("SUBMISSION_TEST_MYSQL_DSN")
	minioEndpoint := os.Getenv("PROBLEM_TEST_MINIO_ENDPOINT")
	judgeEndpoint := os.Getenv("JUDGE_TEST_GRPC_ENDPOINT")
	privateKeyFile := os.Getenv("JUDGE_TEST_GATEWAY_PRIVATE_KEY_FILE")
	if baseURL == "" || userMySQLDSN == "" || problemMySQLDSN == "" || submissionMySQLDSN == "" || minioEndpoint == "" || judgeEndpoint == "" || privateKeyFile == "" {
		t.Skip("set Gateway, MySQL, MinIO, Judge gRPC and signing-key integration settings")
	}

	api := apiClient{baseURL: baseURL, client: &http.Client{Timeout: 10 * time.Second}}
	userDB := openIntegrationDB(t, userMySQLDSN)
	problemDB := openIntegrationDB(t, problemMySQLDSN)
	submissionDB := openIntegrationDB(t, submissionMySQLDSN)
	adminID, adminToken := registerJudgeIntegrationUser(t, api, userDB, "judgeadmin", true)
	problemID := createJudgeIntegrationProblem(t, api, adminToken)
	userID, _ := registerJudgeIntegrationUser(t, api, userDB, "judgeuser", false)

	client := newJudgeIntegrationClient(t, judgeEndpoint, privateKeyFile)
	userContext := judgeActorContext(t.Context(), userID, "user")
	adminContext := judgeActorContext(t.Context(), adminID, "admin")
	otherContext := judgeActorContext(t.Context(), userID+10_000, "user")

	if _, err := client.GetSubmission(t.Context(), &submissionv1.GetSubmissionRequest{SubmissionId: 1}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("unsigned actor GetSubmission status = %v, want %v", status.Code(err), codes.Unauthenticated)
	}

	source := []byte("package main\n\nfunc main() {}\n")
	idempotencyKey := uuid.NewString()
	createRequest := &submissionv1.CreateSubmissionRequest{
		ProblemId: problemID, Language: "go", SourceCode: string(source), IdempotencyKey: idempotencyKey,
	}
	created, err := client.CreateSubmission(userContext, createRequest)
	if err != nil {
		t.Fatalf("CreateSubmission() error = %v", err)
	}
	if created.GetSubmissionId() <= 0 || created.GetStatus() != submissionv1.SubmissionStatus_SUBMISSION_STATUS_QUEUED {
		t.Fatalf("CreateSubmission() = %+v", created)
	}
	replayed, err := client.CreateSubmission(userContext, createRequest)
	if err != nil || replayed.GetSubmissionId() != created.GetSubmissionId() {
		t.Fatalf("CreateSubmission() replay = %+v, %v", replayed, err)
	}
	conflict := &submissionv1.CreateSubmissionRequest{
		ProblemId: problemID, Language: "go", SourceCode: string(source) + "// changed\n", IdempotencyKey: idempotencyKey,
	}
	if _, err = client.CreateSubmission(userContext, conflict); status.Code(err) != codes.Aborted {
		t.Fatalf("idempotency conflict status = %v, want %v", status.Code(err), codes.Aborted)
	}

	second, err := client.CreateSubmission(userContext, &submissionv1.CreateSubmissionRequest{
		ProblemId: problemID, Language: "go", SourceCode: string(source) + "// second\n", IdempotencyKey: uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("second CreateSubmission() error = %v", err)
	}

	got, err := client.GetSubmission(userContext, &submissionv1.GetSubmissionRequest{SubmissionId: created.GetSubmissionId()})
	if err != nil {
		t.Fatalf("GetSubmission() error = %v", err)
	}
	assertJudgeSubmission(t, got.GetSubmission(), created.GetSubmissionId(), userID, problemID)
	if _, err = client.GetSubmission(otherContext, &submissionv1.GetSubmissionRequest{SubmissionId: created.GetSubmissionId()}); status.Code(err) != codes.NotFound {
		t.Fatalf("cross-user GetSubmission status = %v, want %v", status.Code(err), codes.NotFound)
	}
	if _, err = client.GetSubmission(adminContext, &submissionv1.GetSubmissionRequest{SubmissionId: created.GetSubmissionId()}); err != nil {
		t.Fatalf("admin GetSubmission() error = %v", err)
	}

	listed, err := client.ListSubmissions(userContext, &submissionv1.ListSubmissionsRequest{
		Page: &commonv1.PageRequest{Page: 1, PageSize: 1000}, ProblemId: problemID,
		Status: submissionv1.SubmissionStatus_SUBMISSION_STATUS_QUEUED, Language: "go",
	})
	if err != nil {
		t.Fatalf("ListSubmissions() error = %v", err)
	}
	if listed.GetPage().GetPageSize() != 100 || listed.GetPage().GetTotal() != 2 || len(listed.GetItems()) != 2 {
		t.Fatalf("ListSubmissions() page = %+v items=%d", listed.GetPage(), len(listed.GetItems()))
	}
	if listed.GetItems()[0].GetId() != second.GetSubmissionId() || listed.GetItems()[1].GetId() != created.GetSubmissionId() {
		t.Fatalf("ListSubmissions() order = %d, %d", listed.GetItems()[0].GetId(), listed.GetItems()[1].GetId())
	}
	if _, err = client.ListSubmissions(userContext, &submissionv1.ListSubmissionsRequest{Page: &commonv1.PageRequest{}, UserId: userID + 1}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("cross-user ListSubmissions status = %v, want %v", status.Code(err), codes.PermissionDenied)
	}
	adminList, err := client.ListSubmissions(adminContext, &submissionv1.ListSubmissionsRequest{Page: &commonv1.PageRequest{}, UserId: userID})
	if err != nil || adminList.GetPage().GetTotal() != 2 {
		t.Fatalf("admin ListSubmissions() = %+v, %v", adminList, err)
	}

	var objectKey, sourceSHA256, judgeRevision, databaseStatus string
	var sourceSize int64
	err = submissionDB.QueryRowContext(t.Context(), `
		SELECT source_object_key, source_sha256, source_size_bytes, judge_revision, status
		FROM submissions WHERE id = ?
	`, created.GetSubmissionId()).Scan(&objectKey, &sourceSHA256, &sourceSize, &judgeRevision, &databaseStatus)
	if err != nil {
		t.Fatalf("query stored submission: %v", err)
	}
	expectedHash := sha256.Sum256(source)
	if sourceSHA256 != hex.EncodeToString(expectedHash[:]) || sourceSize != int64(len(source)) || databaseStatus != "QUEUED" {
		t.Fatalf("stored source metadata hash=%q size=%d status=%q", sourceSHA256, sourceSize, databaseStatus)
	}
	if judgeRevision != problemRevision(t, problemDB, problemID) {
		t.Fatalf("submission judge revision %q does not match active problem revision", judgeRevision)
	}
	assertSubmissionSourceObject(t, minioEndpoint, objectKey, source)
	assertSubmissionAtomicRecords(t, submissionDB, userID, created.GetSubmissionId(), idempotencyKey)
}

func registerJudgeIntegrationUser(t *testing.T, api apiClient, userDB *sql.DB, prefix string, admin bool) (int64, string) {
	t.Helper()
	account := fmt.Sprintf("%s%d", prefix, time.Now().UnixNano())
	password := "correct-password-1"
	var registered registerResponse
	response := api.request(t, http.MethodPost, "/api/v1/auth/register", map[string]string{
		"username": account, "email": account + "@example.com", "password": password,
	}, "")
	assertStatus(t, response, http.StatusOK)
	decodeJSON(t, response.Body, &registered)
	response.Body.Close()
	if admin {
		if _, err := userDB.ExecContext(t.Context(), `INSERT INTO user_roles (user_id, role_id, created_at) SELECT ?, id, UTC_TIMESTAMP(3) FROM roles WHERE name='admin'`, registered.User.ID); err != nil {
			t.Fatalf("grant integration admin role: %v", err)
		}
	}
	var tokens tokenPair
	response = api.request(t, http.MethodPost, "/api/v1/auth/login", map[string]string{"account": account, "password": password}, "")
	assertStatus(t, response, http.StatusOK)
	decodeJSON(t, response.Body, &tokens)
	response.Body.Close()
	return registered.User.ID, tokens.AccessToken
}

func createJudgeIntegrationProblem(t *testing.T, api apiClient, adminToken string) int64 {
	t.Helper()
	unique := time.Now().UnixNano()
	request := map[string]any{"problem": map[string]any{
		"title": "Judge Integration A+B", "slug": fmt.Sprintf("judge-integration-a-plus-b-%d", unique),
		"description": "Add two integers.", "difficulty": 1, "time_limit_ms": 1000,
		"memory_limit_kb": 65536, "tags": []string{"integration"},
	}}
	var created struct {
		Problem struct {
			ID int64 `json:"id"`
		} `json:"problem"`
	}
	response := api.request(t, http.MethodPost, "/api/v1/problems", request, adminToken)
	assertStatus(t, response, http.StatusOK)
	decodeJSON(t, response.Body, &created)
	response.Body.Close()
	response = problemMultipartRequest(t, api.baseURL, created.Problem.ID, adminToken, 1, []byte("1 2\n"), []byte("3\n"))
	assertStatus(t, response, http.StatusOK)
	response.Body.Close()
	return created.Problem.ID
}

func newJudgeIntegrationClient(t *testing.T, endpoint, privateKeyFile string) submissionv1.SubmissionServiceClient {
	t.Helper()
	privateKey, err := os.ReadFile(privateKeyFile)
	if err != nil {
		t.Fatalf("read integration signing key: %v", err)
	}
	signer, err := internalauth.NewSigner(privateKey, "gateway-internal-2026-09", "go-oj-gateway", "judge-service", "gateway-service", 30*time.Second, nil)
	if err != nil {
		t.Fatalf("create integration signer: %v", err)
	}
	interceptor := internalauth.UnaryClientInterceptor(signer, func(ctx context.Context) internalauth.Actor {
		principal, _ := internalauth.PrincipalFromContext(ctx)
		return internalauth.Actor{ID: principal.ActorID, Roles: append([]string(nil), principal.ActorRoles...), RequestID: principal.RequestID, TraceID: principal.TraceID}
	})
	connection, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(interceptor))
	if err != nil {
		t.Fatalf("connect to judge-service: %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	return submissionv1.NewSubmissionServiceClient(connection)
}

func judgeActorContext(ctx context.Context, actorID int64, roles ...string) context.Context {
	return internalauth.WithPrincipal(ctx, internalauth.Principal{ActorID: actorID, ActorRoles: roles, RequestID: uuid.NewString(), TraceID: uuid.NewString()})
}

func assertJudgeSubmission(t *testing.T, submission *submissionv1.Submission, submissionID, userID, problemID int64) {
	t.Helper()
	if submission == nil || submission.GetId() != submissionID || submission.GetUserId() != userID || submission.GetProblemId() != problemID || submission.GetLanguage() != "go" || submission.GetStatus() != submissionv1.SubmissionStatus_SUBMISSION_STATUS_QUEUED || len(submission.GetJudgeRevision()) != 26 || submission.GetCreatedAt() == nil || submission.GetUpdatedAt() == nil {
		t.Fatalf("unexpected submission: %+v", submission)
	}
}

func assertSubmissionSourceObject(t *testing.T, endpoint, key string, expected []byte) {
	t.Helper()
	client, err := minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4(
		integrationEnvOr("PROBLEM_TEST_MINIO_ACCESS_KEY", "minioadmin"),
		integrationEnvOr("PROBLEM_TEST_MINIO_SECRET_KEY", "minioadmin"), "",
	)})
	if err != nil {
		t.Fatalf("create MinIO client: %v", err)
	}
	object, err := client.GetObject(t.Context(), "submission-source", key, minio.GetObjectOptions{})
	if err != nil {
		t.Fatalf("get submission source object: %v", err)
	}
	defer object.Close()
	content, err := io.ReadAll(object)
	if err != nil {
		t.Fatalf("read submission source object: %v", err)
	}
	if !bytes.Equal(content, expected) {
		t.Fatalf("submission source content = %q, want %q", content, expected)
	}
}

func assertSubmissionAtomicRecords(t *testing.T, db *sql.DB, userID, submissionID int64, idempotencyKey string) {
	t.Helper()
	var submissionCount, outboxCount, idempotencyCount int
	if err := db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM submissions WHERE id = ? AND user_id = ?`, submissionID, userID).Scan(&submissionCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM outbox_events WHERE aggregate_id = ? AND event_type = 'judge.requested'`, submissionID).Scan(&outboxCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM idempotency_requests WHERE actor_id = ? AND operation = 'CreateSubmission' AND idempotency_key = ? AND response IS NOT NULL`, userID, idempotencyKey).Scan(&idempotencyCount); err != nil {
		t.Fatal(err)
	}
	if submissionCount != 1 || outboxCount != 1 || idempotencyCount != 1 {
		t.Fatalf("atomic records submissions=%d outbox=%d idempotency=%d", submissionCount, outboxCount, idempotencyCount)
	}
}

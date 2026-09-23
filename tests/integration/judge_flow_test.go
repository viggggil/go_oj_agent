package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/rabbitmq/amqp091-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"
	submissionv1 "github.com/viggggil/go_oj_agent/api/submission/v1"
	"github.com/viggggil/go_oj_agent/pkg/internalauth"
	judgecontract "github.com/viggggil/go_oj_agent/pkg/judge"
	"github.com/viggggil/go_oj_agent/pkg/mq"
)

func TestJudgeSubmissionFlow(t *testing.T) {
	baseURL := os.Getenv("AUTH_INTEGRATION_BASE_URL")
	userMySQLDSN := os.Getenv("PROBLEM_TEST_USER_MYSQL_DSN")
	problemMySQLDSN := os.Getenv("PROBLEM_TEST_MYSQL_DSN")
	submissionMySQLDSN := os.Getenv("SUBMISSION_TEST_MYSQL_DSN")
	minioEndpoint := os.Getenv("PROBLEM_TEST_MINIO_ENDPOINT")
	judgeEndpoint := os.Getenv("JUDGE_TEST_GRPC_ENDPOINT")
	privateKeyFile := os.Getenv("JUDGE_TEST_GATEWAY_PRIVATE_KEY_FILE")
	rabbitURL := os.Getenv("JUDGE_TEST_RABBITMQ_URL")
	if baseURL == "" || userMySQLDSN == "" || problemMySQLDSN == "" || submissionMySQLDSN == "" || minioEndpoint == "" || judgeEndpoint == "" || privateKeyFile == "" || rabbitURL == "" {
		t.Skip("set Gateway, MySQL, MinIO, RabbitMQ, Judge gRPC and signing-key integration settings")
	}

	api := apiClient{baseURL: baseURL, client: &http.Client{Timeout: 10 * time.Second}}
	userDB := openIntegrationDB(t, userMySQLDSN)
	problemDB := openIntegrationDB(t, problemMySQLDSN)
	submissionDB := openIntegrationDB(t, submissionMySQLDSN)
	adminID, adminToken := registerJudgeIntegrationUser(t, api, userDB, "judgeadmin", true)
	problemID := createJudgeIntegrationProblem(t, api, adminToken)
	assertJudgeRevisionManifest(t, minioEndpoint, problemID, problemRevision(t, problemDB, problemID), 1000, 65536)
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

	judgeResult, err := client.GetJudgeResult(userContext, &submissionv1.GetJudgeResultRequest{SubmissionId: created.GetSubmissionId()})
	if err != nil {
		t.Fatalf("owner GetJudgeResult() error = %v", err)
	}
	if judgeResult.GetResult().GetSubmissionId() != created.GetSubmissionId() ||
		judgeResult.GetResult().GetStatus() != submissionv1.SubmissionStatus_SUBMISSION_STATUS_QUEUED ||
		judgeResult.GetResult().GetVerdict() != submissionv1.JudgeVerdict_JUDGE_VERDICT_UNSPECIFIED ||
		len(judgeResult.GetResult().GetCaseResults()) != 0 ||
		judgeResult.GetResult().GetJudgeRevision() != got.GetSubmission().GetJudgeRevision() {
		t.Fatalf("owner GetJudgeResult() = %+v", judgeResult.GetResult())
	}
	if _, err = client.GetJudgeResult(otherContext, &submissionv1.GetJudgeResultRequest{SubmissionId: created.GetSubmissionId()}); status.Code(err) != codes.NotFound {
		t.Fatalf("cross-user GetJudgeResult status = %v, want %v", status.Code(err), codes.NotFound)
	}
	if _, err = client.GetJudgeResult(adminContext, &submissionv1.GetJudgeResultRequest{SubmissionId: created.GetSubmissionId()}); err != nil {
		t.Fatalf("admin GetJudgeResult() error = %v", err)
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
	requested := waitForRelayMessage(t, rabbitURL, mq.RoutingJudgeTaskGo, mq.EventTypeJudgeTask, created.GetSubmissionId())
	if requested.MessageID != requested.EventID || requested.RoutingKey != "judge.task.go" {
		t.Fatalf("judge task metadata = %+v", requested)
	}
	waitForOutboxStatus(t, submissionDB, requested.EventID, "published")

	rejudgeKey := uuid.NewString()
	rejudged, err := client.RejudgeSubmission(adminContext, &submissionv1.RejudgeSubmissionRequest{
		SubmissionId: created.GetSubmissionId(), IdempotencyKey: rejudgeKey,
	})
	if err != nil {
		t.Fatalf("RejudgeSubmission() error = %v", err)
	}
	newSubmission := rejudged.GetSubmission()
	if rejudged.GetInvalidatedSubmissionId() != created.GetSubmissionId() || newSubmission == nil ||
		newSubmission.GetId() <= 0 || newSubmission.GetId() == created.GetSubmissionId() ||
		newSubmission.GetUserId() != userID || newSubmission.GetProblemId() != problemID ||
		newSubmission.GetLanguage() != "go" || newSubmission.GetStatus() != submissionv1.SubmissionStatus_SUBMISSION_STATUS_QUEUED ||
		newSubmission.GetJudgeRevision() != problemRevision(t, problemDB, problemID) {
		t.Fatalf("RejudgeSubmission() = %+v", rejudged)
	}
	replayRejudge, err := client.RejudgeSubmission(adminContext, &submissionv1.RejudgeSubmissionRequest{
		SubmissionId: created.GetSubmissionId(), IdempotencyKey: rejudgeKey,
	})
	if err != nil || replayRejudge.GetInvalidatedSubmissionId() != created.GetSubmissionId() || replayRejudge.GetSubmission().GetId() != newSubmission.GetId() {
		t.Fatalf("RejudgeSubmission() replay = %+v, %v", replayRejudge, err)
	}
	if _, err = client.RejudgeSubmission(userContext, &submissionv1.RejudgeSubmissionRequest{
		SubmissionId: created.GetSubmissionId(), IdempotencyKey: uuid.NewString(),
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("ordinary user RejudgeSubmission status = %v, want %v", status.Code(err), codes.PermissionDenied)
	}
	if _, err = client.RejudgeSubmission(adminContext, &submissionv1.RejudgeSubmissionRequest{
		SubmissionId: created.GetSubmissionId(), IdempotencyKey: uuid.NewString(),
	}); status.Code(err) != codes.Aborted {
		t.Fatalf("second RejudgeSubmission status = %v, want %v", status.Code(err), codes.Aborted)
	}

	var oldStatus, replacementStatus, replacementKey, replacementHash string
	var replacementSize int64
	if err = submissionDB.QueryRowContext(t.Context(), `SELECT status FROM submissions WHERE id = ?`, created.GetSubmissionId()).Scan(&oldStatus); err != nil {
		t.Fatalf("query invalidated submission: %v", err)
	}
	if err = submissionDB.QueryRowContext(t.Context(), `
		SELECT status, source_object_key, source_sha256, source_size_bytes
		FROM submissions WHERE id = ?
	`, newSubmission.GetId()).Scan(&replacementStatus, &replacementKey, &replacementHash, &replacementSize); err != nil {
		t.Fatalf("query replacement submission: %v", err)
	}
	if oldStatus != "INVALIDATED" || replacementStatus != "QUEUED" || replacementKey != objectKey || replacementHash != sourceSHA256 || replacementSize != sourceSize {
		t.Fatalf("rejudge source/status old=%q replacement=%q key=%q hash=%q size=%d", oldStatus, replacementStatus, replacementKey, replacementHash, replacementSize)
	}
	var invalidatedEvents, requestedEvents int
	if err = submissionDB.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM outbox_events WHERE aggregate_id = ? AND event_type = 'submission.invalidated'`, created.GetSubmissionId()).Scan(&invalidatedEvents); err != nil {
		t.Fatalf("query invalidated outbox: %v", err)
	}
	if err = submissionDB.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM outbox_events WHERE aggregate_id = ? AND event_type = 'judge.requested'`, newSubmission.GetId()).Scan(&requestedEvents); err != nil {
		t.Fatalf("query replacement outbox: %v", err)
	}
	if invalidatedEvents != 1 || requestedEvents != 1 {
		t.Fatalf("rejudge outbox invalidated=%d requested=%d", invalidatedEvents, requestedEvents)
	}
	var rejudgeIdempotencyCount int
	if err = submissionDB.QueryRowContext(t.Context(), `
		SELECT COUNT(*) FROM idempotency_requests
		WHERE actor_id = ? AND operation = 'RejudgeSubmission' AND idempotency_key = ? AND response IS NOT NULL
	`, adminID, rejudgeKey).Scan(&rejudgeIdempotencyCount); err != nil {
		t.Fatalf("query rejudge idempotency: %v", err)
	}
	if rejudgeIdempotencyCount != 1 {
		t.Fatalf("rejudge idempotency records = %d, want 1", rejudgeIdempotencyCount)
	}
	replacementRequested := waitForRelayMessage(t, rabbitURL, mq.RoutingJudgeTaskGo, mq.EventTypeJudgeTask, newSubmission.GetId())
	invalidated := waitForRelayMessage(t, rabbitURL, "submission.invalidated", "submission.invalidated", created.GetSubmissionId())
	waitForOutboxStatus(t, submissionDB, replacementRequested.EventID, "published")
	waitForOutboxStatus(t, submissionDB, invalidated.EventID, "published")
}

type relayMessage struct {
	EventID    string
	MessageID  string
	RoutingKey string
}

func waitForRelayMessage(t *testing.T, rabbitURL, queue, eventType string, submissionID int64) relayMessage {
	t.Helper()
	connection, err := amqp091.Dial(rabbitURL)
	if err != nil {
		t.Fatalf("connect RabbitMQ: %v", err)
	}
	defer connection.Close()
	deadline := time.Now().Add(10 * time.Second)
	channel := waitForRabbitQueue(t, connection, queue, deadline)
	defer channel.Close()
	for time.Now().Before(deadline) {
		delivery, ok, getErr := channel.Get(queue, true)
		if getErr != nil {
			t.Fatalf("get RabbitMQ message from %s: %v", queue, getErr)
		}
		if !ok {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		var envelope struct {
			EventID   string          `json:"event_id"`
			EventType string          `json:"event_type"`
			Data      json.RawMessage `json:"data"`
		}
		var payload struct {
			SubmissionID int64 `json:"submission_id"`
		}
		if json.Unmarshal(delivery.Body, &envelope) != nil || json.Unmarshal(envelope.Data, &payload) != nil {
			t.Fatalf("decode RabbitMQ message: %s", delivery.Body)
		}
		if envelope.EventType != eventType || payload.SubmissionID != submissionID {
			continue
		}
		if delivery.DeliveryMode != amqp091.Persistent || delivery.MessageId == "" || envelope.EventID == "" || delivery.Type != eventType {
			t.Fatalf("RabbitMQ delivery metadata = %+v envelope=%+v", delivery, envelope)
		}
		if bytes.Contains(delivery.Body, []byte("package main")) || bytes.Contains(delivery.Body, []byte("minioadmin")) {
			t.Fatalf("RabbitMQ delivery leaked source or credentials: %s", delivery.Body)
		}
		return relayMessage{EventID: envelope.EventID, MessageID: delivery.MessageId, RoutingKey: delivery.RoutingKey}
	}
	t.Fatalf("timed out waiting for %s submission %d", eventType, submissionID)
	return relayMessage{}
}

func waitForRabbitQueue(t *testing.T, connection *amqp091.Connection, queue string, deadline time.Time) *amqp091.Channel {
	t.Helper()
	for time.Now().Before(deadline) {
		channel, err := connection.Channel()
		if err != nil {
			t.Fatalf("open RabbitMQ channel: %v", err)
		}
		if _, err = channel.QueueDeclarePassive(queue, true, false, false, false, nil); err == nil {
			return channel
		}
		_ = channel.Close()
		var rabbitErr *amqp091.Error
		if !errors.As(err, &rabbitErr) || rabbitErr.Code != 404 {
			t.Fatalf("inspect RabbitMQ queue %s: %v", queue, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for RabbitMQ queue %s", queue)
	return nil
}

func waitForOutboxStatus(t *testing.T, db *sql.DB, eventID, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var status string
		if err := db.QueryRowContext(t.Context(), `SELECT status FROM outbox_events WHERE event_id = ?`, eventID).Scan(&status); err == nil && status == want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("outbox event %s did not reach status %s", eventID, want)
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

func assertJudgeRevisionManifest(t *testing.T, endpoint string, problemID int64, revision string, timeLimitMS, memoryLimitKB int32) {
	t.Helper()
	client, err := minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4(
		integrationEnvOr("PROBLEM_TEST_MINIO_ACCESS_KEY", "minioadmin"),
		integrationEnvOr("PROBLEM_TEST_MINIO_SECRET_KEY", "minioadmin"), "",
	)})
	if err != nil {
		t.Fatalf("create MinIO client: %v", err)
	}
	key, err := judgecontract.ManifestObjectKey(problemID, revision)
	if err != nil {
		t.Fatalf("build judge manifest key: %v", err)
	}
	object, err := client.GetObject(t.Context(), "problem-data", key, minio.GetObjectOptions{})
	if err != nil {
		t.Fatalf("get judge manifest: %v", err)
	}
	defer object.Close()
	content, err := io.ReadAll(object)
	if err != nil {
		t.Fatalf("read judge manifest: %v", err)
	}
	var manifest judgecontract.Manifest
	if err = json.Unmarshal(content, &manifest); err != nil {
		t.Fatalf("invalid judge manifest %s: %v", content, err)
	}
	if err = manifest.Validate(); err != nil {
		t.Fatalf("invalid judge manifest %s: %v", content, err)
	}
	if manifest.ProblemID != problemID || manifest.JudgeRevision != revision || manifest.TimeLimitMS != timeLimitMS || manifest.MemoryLimitKB != memoryLimitKB || len(manifest.Testcases) != 1 {
		t.Fatalf("judge manifest = %+v", manifest)
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

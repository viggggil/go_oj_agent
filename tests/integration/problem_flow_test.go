package integration_test

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/oklog/ulid/v2"
	"github.com/redis/go-redis/v9"
)

const problemBucket = "problem-data"

type uploadedTestcase struct {
	ID              int64  `json:"id"`
	ProblemID       int64  `json:"problem_id"`
	CaseNo          int32  `json:"case_no"`
	InputObjectKey  string `json:"input_object_key"`
	OutputObjectKey string `json:"output_object_key"`
	InputSHA256     string `json:"input_sha256"`
	OutputSHA256    string `json:"output_sha256"`
	InputSizeBytes  int64  `json:"input_size_bytes"`
	OutputSizeBytes int64  `json:"output_size_bytes"`
	Status          int32  `json:"status"`
}

type judgeManifest struct {
	ProblemID     int64  `json:"problem_id"`
	JudgeRevision string `json:"judge_revision"`
	Testcases     []struct {
		CaseNo int32 `json:"case_no"`
		Input  struct {
			ObjectKey string `json:"object_key"`
			SHA256    string `json:"sha256"`
			Size      int64  `json:"size_bytes"`
		} `json:"input"`
		Output struct {
			ObjectKey string `json:"object_key"`
			SHA256    string `json:"sha256"`
			Size      int64  `json:"size_bytes"`
		} `json:"output"`
	} `json:"testcases"`
}

func TestProblemManagementFlow(t *testing.T) {
	baseURL := os.Getenv("AUTH_INTEGRATION_BASE_URL")
	userMySQLDSN := os.Getenv("PROBLEM_TEST_USER_MYSQL_DSN")
	problemMySQLDSN := os.Getenv("PROBLEM_TEST_MYSQL_DSN")
	redisAddr := os.Getenv("PROBLEM_TEST_REDIS_ADDR")
	minioEndpoint := os.Getenv("PROBLEM_TEST_MINIO_ENDPOINT")
	if baseURL == "" || userMySQLDSN == "" || problemMySQLDSN == "" || redisAddr == "" || minioEndpoint == "" {
		t.Skip("set integration base URL, MySQL DSNs, Redis address and MinIO endpoint")
	}

	api := apiClient{baseURL: baseURL, client: &http.Client{Timeout: 10 * time.Second}}
	userDB := openIntegrationDB(t, userMySQLDSN)
	problemDB := openIntegrationDB(t, problemMySQLDSN)
	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
	t.Cleanup(func() { _ = redisClient.Close() })
	minioClient, err := minio.New(minioEndpoint, &minio.Options{
		Creds: credentials.NewStaticV4(
			integrationEnvOr("PROBLEM_TEST_MINIO_ACCESS_KEY", "minioadmin"),
			integrationEnvOr("PROBLEM_TEST_MINIO_SECRET_KEY", "minioadmin"),
			"",
		),
	})
	if err != nil {
		t.Fatalf("create MinIO client: %v", err)
	}

	unique := time.Now().UnixNano()
	account := fmt.Sprintf("problemadmin%d", unique)
	password := "correct-password-1"
	var registered registerResponse
	response := api.request(t, http.MethodPost, "/api/v1/auth/register", map[string]string{"username": account, "email": account + "@example.com", "password": password}, "")
	assertStatus(t, response, http.StatusOK)
	decodeJSON(t, response.Body, &registered)
	response.Body.Close()

	_, err = userDB.ExecContext(t.Context(), `INSERT INTO user_roles (user_id, role_id, created_at) SELECT ?, id, UTC_TIMESTAMP(3) FROM roles WHERE name='admin'`, registered.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	var tokens tokenPair
	response = api.request(t, http.MethodPost, "/api/v1/auth/login", map[string]string{"account": account, "password": password}, "")
	assertStatus(t, response, http.StatusOK)
	decodeJSON(t, response.Body, &tokens)
	response.Body.Close()

	create := map[string]any{"problem": map[string]any{"title": "Integration A+B", "slug": fmt.Sprintf("integration-a-plus-b-%d", unique), "description": "Add two integers.", "difficulty": 1, "time_limit_ms": 1000, "memory_limit_kb": 65536, "tags": []string{"math"}}}
	var created struct {
		Problem struct {
			ID int64 `json:"id"`
		} `json:"problem"`
	}
	response = api.request(t, http.MethodPost, "/api/v1/problems", create, tokens.AccessToken)
	assertStatus(t, response, http.StatusOK)
	decodeJSON(t, response.Body, &created)
	response.Body.Close()
	if created.Problem.ID <= 0 {
		t.Fatal("create problem did not return an id")
	}

	cacheKey := fmt.Sprintf("%s:problem:%d", integrationEnvOr("PROBLEM_TEST_REDIS_NAMESPACE", "go_oj_agent:problem"), created.Problem.ID)
	if exists, err := redisClient.Exists(t.Context(), cacheKey).Result(); err != nil || exists != 1 {
		t.Fatalf("problem cache after create: exists=%d err=%v", exists, err)
	}

	firstInput, firstOutput := []byte("1 2\n"), []byte("3\n")
	first := uploadProblemTestcase(t, api.baseURL, created.Problem.ID, tokens.AccessToken, 1, firstInput, firstOutput)
	assertUploadedTestcase(t, first, created.Problem.ID, 1, firstInput, firstOutput)
	assertMinIOObject(t, minioClient, first.InputObjectKey, firstInput)
	assertMinIOObject(t, minioClient, first.OutputObjectKey, firstOutput)

	firstRevision := problemRevision(t, problemDB, created.Problem.ID)
	firstManifest := loadJudgeManifest(t, minioClient, created.Problem.ID, firstRevision)
	assertManifestCases(t, firstManifest, created.Problem.ID, firstRevision, []int32{1})
	assertManifestObjects(t, minioClient, firstManifest)

	secondInput, secondOutput := []byte("2 3\n"), []byte("5\n")
	second := uploadProblemTestcase(t, api.baseURL, created.Problem.ID, tokens.AccessToken, 2, secondInput, secondOutput)
	assertUploadedTestcase(t, second, created.Problem.ID, 2, secondInput, secondOutput)
	secondRevision := problemRevision(t, problemDB, created.Problem.ID)
	if secondRevision == firstRevision {
		t.Fatal("adding a testcase did not publish a new judge revision")
	}
	secondManifest := loadJudgeManifest(t, minioClient, created.Problem.ID, secondRevision)
	assertManifestCases(t, secondManifest, created.Problem.ID, secondRevision, []int32{1, 2})
	assertManifestObjects(t, minioClient, secondManifest)

	response = api.request(t, http.MethodGet, fmt.Sprintf("/api/v1/problems/%d", created.Problem.ID), nil, tokens.AccessToken)
	assertStatus(t, response, http.StatusOK)
	response.Body.Close()
	response = api.request(t, http.MethodGet, fmt.Sprintf("/api/v1/problems/%d/testcases", created.Problem.ID), nil, tokens.AccessToken)
	assertStatus(t, response, http.StatusOK)
	response.Body.Close()

	response = api.request(t, http.MethodDelete, fmt.Sprintf("/api/v1/problems/%d/testcases/%d", created.Problem.ID, first.ID), nil, tokens.AccessToken)
	assertStatus(t, response, http.StatusOK)
	response.Body.Close()
	archivedRevision := problemRevision(t, problemDB, created.Problem.ID)
	if archivedRevision == secondRevision {
		t.Fatal("archiving a testcase did not publish a new judge revision")
	}
	archivedManifest := loadJudgeManifest(t, minioClient, created.Problem.ID, archivedRevision)
	assertManifestCases(t, archivedManifest, created.Problem.ID, archivedRevision, []int32{2})
	assertManifestObjects(t, minioClient, archivedManifest)
	assertMinIOObject(t, minioClient, first.InputObjectKey, firstInput)
	assertMinIOObject(t, minioClient, first.OutputObjectKey, firstOutput)

	response = api.request(t, http.MethodDelete, fmt.Sprintf("/api/v1/problems/%d", created.Problem.ID), nil, tokens.AccessToken)
	assertStatus(t, response, http.StatusOK)
	response.Body.Close()
	var status string
	if err = problemDB.QueryRowContext(t.Context(), `SELECT status FROM problems WHERE id = ?`, created.Problem.ID).Scan(&status); err != nil {
		t.Fatalf("query archived problem: %v", err)
	}
	if status != "PROBLEM_STATUS_ARCHIVED" {
		t.Fatalf("problem status after archive = %q", status)
	}
	if exists, err := redisClient.Exists(t.Context(), cacheKey).Result(); err != nil || exists != 0 {
		t.Fatalf("problem cache after archive: exists=%d err=%v", exists, err)
	}
}

func openIntegrationDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	return db
}

func uploadProblemTestcase(t *testing.T, baseURL string, problemID int64, token string, caseNo int32, inputContent, outputContent []byte) uploadedTestcase {
	t.Helper()
	response := problemMultipartRequest(t, baseURL, problemID, token, caseNo, inputContent, outputContent)
	defer response.Body.Close()
	assertStatus(t, response, http.StatusOK)
	var uploaded struct {
		Testcase uploadedTestcase `json:"testcase"`
	}
	decodeJSON(t, response.Body, &uploaded)
	return uploaded.Testcase
}

func problemMultipartRequest(t *testing.T, baseURL string, problemID int64, token string, caseNo int32, inputContent, outputContent []byte) *http.Response {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("case_no", strconv.FormatInt(int64(caseNo), 10)); err != nil {
		t.Fatal(err)
	}
	input, err := writer.CreateFormFile("input", fmt.Sprintf("%d.in", caseNo))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = input.Write(inputContent); err != nil {
		t.Fatal(err)
	}
	output, err := writer.CreateFormFile("output", fmt.Sprintf("%d.out", caseNo))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = output.Write(outputContent); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, fmt.Sprintf("%s/api/v1/problems/%d/testcases/upload", baseURL, problemID), &body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func assertUploadedTestcase(t *testing.T, testcase uploadedTestcase, problemID int64, caseNo int32, input, output []byte) {
	t.Helper()
	if testcase.ID <= 0 || testcase.ProblemID != problemID || testcase.CaseNo != caseNo || testcase.Status != 1 {
		t.Fatalf("unexpected testcase metadata: %+v", testcase)
	}
	inputHash, outputHash := sha256.Sum256(input), sha256.Sum256(output)
	if testcase.InputSHA256 != hex.EncodeToString(inputHash[:]) || testcase.OutputSHA256 != hex.EncodeToString(outputHash[:]) || testcase.InputSizeBytes != int64(len(input)) || testcase.OutputSizeBytes != int64(len(output)) {
		t.Fatalf("unexpected testcase integrity metadata: %+v", testcase)
	}
}

func problemRevision(t *testing.T, db *sql.DB, problemID int64) string {
	t.Helper()
	var revision string
	if err := db.QueryRowContext(t.Context(), `SELECT active_judge_revision FROM problems WHERE id = ?`, problemID).Scan(&revision); err != nil {
		t.Fatalf("query active judge revision: %v", err)
	}
	if _, err := ulid.ParseStrict(revision); err != nil {
		t.Fatalf("active judge revision %q is not a ULID: %v", revision, err)
	}
	return revision
}

func loadJudgeManifest(t *testing.T, client *minio.Client, problemID int64, revision string) judgeManifest {
	t.Helper()
	key := fmt.Sprintf("problem-%d/judge-revisions/%s/manifest.json", problemID, revision)
	content := readMinIOObject(t, client, key)
	var manifest judgeManifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		t.Fatalf("decode judge manifest %q: %v", key, err)
	}
	return manifest
}

func assertManifestCases(t *testing.T, manifest judgeManifest, problemID int64, revision string, caseNumbers []int32) {
	t.Helper()
	if manifest.ProblemID != problemID || manifest.JudgeRevision != revision || len(manifest.Testcases) != len(caseNumbers) {
		t.Fatalf("unexpected judge manifest: %+v", manifest)
	}
	for index, caseNo := range caseNumbers {
		if manifest.Testcases[index].CaseNo != caseNo {
			t.Fatalf("manifest testcase %d has case number %d, want %d", index, manifest.Testcases[index].CaseNo, caseNo)
		}
	}
}

func assertManifestObjects(t *testing.T, client *minio.Client, manifest judgeManifest) {
	t.Helper()
	for _, testcase := range manifest.Testcases {
		assertMinIOIntegrity(t, client, testcase.Input.ObjectKey, testcase.Input.SHA256, testcase.Input.Size)
		assertMinIOIntegrity(t, client, testcase.Output.ObjectKey, testcase.Output.SHA256, testcase.Output.Size)
	}
}

func assertMinIOObject(t *testing.T, client *minio.Client, key string, expected []byte) {
	t.Helper()
	actual := readMinIOObject(t, client, key)
	if !bytes.Equal(actual, expected) {
		t.Fatalf("MinIO object %q content = %q, want %q", key, actual, expected)
	}
}

func assertMinIOIntegrity(t *testing.T, client *minio.Client, key, expectedHash string, expectedSize int64) {
	t.Helper()
	content := readMinIOObject(t, client, key)
	hash := sha256.Sum256(content)
	if int64(len(content)) != expectedSize || hex.EncodeToString(hash[:]) != expectedHash {
		t.Fatalf("MinIO object %q failed integrity check", key)
	}
}

func readMinIOObject(t *testing.T, client *minio.Client, key string) []byte {
	t.Helper()
	object, err := client.GetObject(t.Context(), problemBucket, key, minio.GetObjectOptions{})
	if err != nil {
		t.Fatalf("get MinIO object %q: %v", key, err)
	}
	defer object.Close()
	content, err := io.ReadAll(object)
	if err != nil {
		t.Fatalf("read MinIO object %q: %v", key, err)
	}
	return content
}

func integrationEnvOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

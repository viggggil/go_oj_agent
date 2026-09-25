package e2e_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
	"testing"

	_ "github.com/go-sql-driver/mysql"
	"github.com/oklog/ulid/v2"
)

type apiClient struct {
	baseURL string
	client  *http.Client
}

type user struct {
	ID       int64    `json:"id"`
	Username string   `json:"username"`
	Email    string   `json:"email"`
	Status   string   `json:"status"`
	Roles    []string `json:"roles"`
}

type registerResponse struct {
	User user `json:"user"`
}

type tokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

func (c apiClient) request(t *testing.T, method, path string, payload any, accessToken string) *http.Response {
	t.Helper()
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("encode request: %v", err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(t.Context(), method, c.baseURL+path, body)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if accessToken != "" {
		request.Header.Set("Authorization", "Bearer "+accessToken)
	}
	response, err := c.client.Do(request)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	return response
}

func assertStatus(t *testing.T, response *http.Response, expected int) {
	t.Helper()
	if response.StatusCode == expected {
		return
	}
	body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	t.Fatalf("expected HTTP %d, got %d: %s", expected, response.StatusCode, body)
}

func decodeJSON(t *testing.T, body io.Reader, target any) {
	t.Helper()
	if err := json.NewDecoder(body).Decode(target); err != nil {
		t.Fatalf("decode response: %v", err)
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

func integrationEnvOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

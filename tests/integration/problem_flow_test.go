package integration_test

import (
	"bytes"
	"database/sql"
	"fmt"
	"mime/multipart"
	"net/http"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func TestProblemManagementFlow(t *testing.T) {
	baseURL := os.Getenv("AUTH_INTEGRATION_BASE_URL")
	mysqlDSN := os.Getenv("PROBLEM_TEST_USER_MYSQL_DSN")
	if baseURL == "" || mysqlDSN == "" {
		t.Skip("set integration base URL and user MySQL DSN")
	}
	api := apiClient{baseURL: baseURL, client: &http.Client{Timeout: 10 * time.Second}}
	unique := time.Now().UnixNano()
	account := fmt.Sprintf("problemadmin%d", unique)
	password := "correct-password-1"
	var registered registerResponse
	response := api.request(t, http.MethodPost, "/api/v1/auth/register", map[string]string{"username": account, "email": account + "@example.com", "password": password}, "")
	assertStatus(t, response, http.StatusOK)
	decodeJSON(t, response.Body, &registered)
	response.Body.Close()

	db, err := sql.Open("mysql", mysqlDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.ExecContext(t.Context(), `INSERT INTO user_roles (user_id, role_id, created_at) SELECT ?, id, UTC_TIMESTAMP(3) FROM roles WHERE name='admin'`, registered.User.ID)
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

	response = problemMultipartRequest(t, api.baseURL, created.Problem.ID, tokens.AccessToken)
	assertStatus(t, response, http.StatusOK)
	var uploaded struct {
		Testcase struct {
			ID     int64 `json:"id"`
			CaseNo int32 `json:"case_no"`
		} `json:"testcase"`
	}
	decodeJSON(t, response.Body, &uploaded)
	response.Body.Close()
	if uploaded.Testcase.ID <= 0 || uploaded.Testcase.CaseNo != 1 {
		t.Fatalf("unexpected testcase: %+v", uploaded.Testcase)
	}

	response = api.request(t, http.MethodGet, fmt.Sprintf("/api/v1/problems/%d", created.Problem.ID), nil, tokens.AccessToken)
	assertStatus(t, response, http.StatusOK)
	response.Body.Close()
	response = api.request(t, http.MethodGet, fmt.Sprintf("/api/v1/problems/%d/testcases", created.Problem.ID), nil, tokens.AccessToken)
	assertStatus(t, response, http.StatusOK)
	response.Body.Close()
	response = api.request(t, http.MethodDelete, fmt.Sprintf("/api/v1/problems/%d/testcases/%d", created.Problem.ID, uploaded.Testcase.ID), nil, tokens.AccessToken)
	assertStatus(t, response, http.StatusOK)
	response.Body.Close()
	response = api.request(t, http.MethodDelete, fmt.Sprintf("/api/v1/problems/%d", created.Problem.ID), nil, tokens.AccessToken)
	assertStatus(t, response, http.StatusOK)
	response.Body.Close()
}

func problemMultipartRequest(t *testing.T, baseURL string, problemID int64, token string) *http.Response {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("case_no", "1"); err != nil {
		t.Fatal(err)
	}
	input, err := writer.CreateFormFile("input", "1.in")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = input.Write([]byte("1 2\n"))
	output, err := writer.CreateFormFile("output", "1.out")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = output.Write([]byte("3\n"))
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

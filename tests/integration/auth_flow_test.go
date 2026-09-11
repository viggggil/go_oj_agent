package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
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

type currentUserResponse struct {
	User user `json:"user"`
}

type errorResponse struct {
	Code   int    `json:"code"`
	Reason string `json:"reason"`
}

func TestAuthenticationFlow(t *testing.T) {
	baseURL := os.Getenv("AUTH_INTEGRATION_BASE_URL")
	if baseURL == "" {
		t.Skip("set AUTH_INTEGRATION_BASE_URL to run the authentication integration test")
	}

	api := apiClient{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 5 * time.Second},
	}
	unique := time.Now().UnixNano()
	username := fmt.Sprintf("integration%d", unique)
	email := fmt.Sprintf("%s@example.com", username)
	password := "correct-password-1"

	t.Run("health", func(t *testing.T) {
		response := api.request(t, http.MethodGet, "/healthz", nil, "")
		defer response.Body.Close()
		assertStatus(t, response, http.StatusOK)
	})

	var registered registerResponse
	t.Run("register", func(t *testing.T) {
		response := api.request(t, http.MethodPost, "/api/v1/auth/register", map[string]string{
			"username": username,
			"email":    email,
			"password": password,
		}, "")
		defer response.Body.Close()
		assertStatus(t, response, http.StatusOK)
		decodeJSON(t, response.Body, &registered)
		if registered.User.ID <= 0 || registered.User.Username != username || registered.User.Email != email {
			t.Fatalf("unexpected registered user: %+v", registered.User)
		}
		if registered.User.Status != "active" || len(registered.User.Roles) != 1 || registered.User.Roles[0] != "user" {
			t.Fatalf("unexpected registered user state: %+v", registered.User)
		}
	})

	t.Run("reject duplicate registration", func(t *testing.T) {
		response := api.request(t, http.MethodPost, "/api/v1/auth/register", map[string]string{
			"username": username,
			"email":    email,
			"password": password,
		}, "")
		defer response.Body.Close()
		assertAPIError(t, response, http.StatusConflict, "USER_ERROR_REASON_ALREADY_EXISTS")
	})

	t.Run("reject invalid password", func(t *testing.T) {
		response := api.request(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
			"account":  email,
			"password": "incorrect-password",
		}, "")
		defer response.Body.Close()
		assertAPIError(t, response, http.StatusUnauthorized, "USER_ERROR_REASON_INVALID_CREDENTIAL")
	})

	t.Run("reject missing and invalid access tokens", func(t *testing.T) {
		response := api.request(t, http.MethodGet, "/api/v1/users/me", nil, "")
		defer response.Body.Close()
		assertAPIError(t, response, http.StatusUnauthorized, "GATEWAY_UNAUTHENTICATED")

		response = api.request(t, http.MethodGet, "/api/v1/users/me", nil, "invalid-token")
		defer response.Body.Close()
		assertAPIError(t, response, http.StatusUnauthorized, "GATEWAY_UNAUTHENTICATED")
	})

	var tokens tokenPair
	t.Run("login", func(t *testing.T) {
		response := api.request(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
			"account":  email,
			"password": password,
		}, "")
		defer response.Body.Close()
		assertStatus(t, response, http.StatusOK)
		decodeJSON(t, response.Body, &tokens)
		assertTokenPair(t, tokens)
		if tokens.ExpiresIn != 2 {
			t.Fatalf("expected 2 second access token TTL, got %d", tokens.ExpiresIn)
		}
	})

	assertCurrentUser := func(t *testing.T, accessToken string) {
		t.Helper()
		response := api.request(t, http.MethodGet, "/api/v1/users/me", nil, accessToken)
		defer response.Body.Close()
		assertStatus(t, response, http.StatusOK)
		var current currentUserResponse
		decodeJSON(t, response.Body, &current)
		if current.User.ID != registered.User.ID || current.User.Username != username || current.User.Email != email {
			t.Fatalf("current user does not match registration: %+v", current.User)
		}
	}

	t.Run("get current user", func(t *testing.T) {
		assertCurrentUser(t, tokens.AccessToken)
	})

	var rotated tokenPair
	t.Run("refresh after access token expires", func(t *testing.T) {
		time.Sleep(3 * time.Second)

		response := api.request(t, http.MethodGet, "/api/v1/users/me", nil, tokens.AccessToken)
		defer response.Body.Close()
		assertAPIError(t, response, http.StatusUnauthorized, "GATEWAY_UNAUTHENTICATED")

		response = api.request(t, http.MethodPost, "/api/v1/auth/refresh", map[string]string{
			"refresh_token": tokens.RefreshToken,
		}, "")
		defer response.Body.Close()
		assertStatus(t, response, http.StatusOK)
		decodeJSON(t, response.Body, &rotated)
		assertTokenPair(t, rotated)
		if rotated.RefreshToken == tokens.RefreshToken {
			t.Fatal("refresh token was not rotated")
		}

		assertCurrentUser(t, rotated.AccessToken)
	})

	t.Run("reject rotated refresh token", func(t *testing.T) {
		response := api.request(t, http.MethodPost, "/api/v1/auth/refresh", map[string]string{
			"refresh_token": tokens.RefreshToken,
		}, "")
		defer response.Body.Close()
		assertAPIError(t, response, http.StatusUnauthorized, "USER_ERROR_REASON_REFRESH_TOKEN_DENIED")
	})
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

func assertAPIError(t *testing.T, response *http.Response, status int, reason string) {
	t.Helper()
	assertStatus(t, response, status)
	var apiError errorResponse
	decodeJSON(t, response.Body, &apiError)
	if apiError.Code != status || apiError.Reason != reason {
		t.Fatalf("expected API error code=%d reason=%q, got %+v", status, reason, apiError)
	}
}

func assertTokenPair(t *testing.T, tokens tokenPair) {
	t.Helper()
	if tokens.AccessToken == "" || tokens.RefreshToken == "" || tokens.ExpiresIn <= 0 {
		t.Fatal("response did not contain a complete token pair")
	}
}

func decodeJSON(t *testing.T, body io.Reader, target any) {
	t.Helper()
	if err := json.NewDecoder(body).Decode(target); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

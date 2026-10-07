package integration_test

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAgentGatewayStreamAndPersistence(t *testing.T) {
	baseURL, dsn := os.Getenv("AGENT_INTEGRATION_BASE_URL"), os.Getenv("AGENT_TEST_MYSQL_DSN")
	if baseURL == "" || dsn == "" {
		t.Skip("set Agent integration configuration")
	}
	api := apiClient{baseURL: baseURL, client: &http.Client{Timeout: 10 * time.Second}}
	account := fmt.Sprintf("agent%d", time.Now().UnixNano())
	register := api.request(t, http.MethodPost, "/api/v1/auth/register", map[string]string{"username": account, "email": account + "@example.com", "password": "correct-password-1"}, "")
	assertStatus(t, register, http.StatusOK)
	var registered registerResponse
	decodeJSON(t, register.Body, &registered)
	register.Body.Close()
	var conversation string
	for _, message := range []string{"介绍算法", "制定学习计划"} {
		login := api.request(t, http.MethodPost, "/api/v1/auth/login", map[string]string{"account": account, "password": "correct-password-1"}, "")
		assertStatus(t, login, http.StatusOK)
		var tokens tokenPair
		decodeJSON(t, login.Body, &tokens)
		login.Body.Close()
		payload := map[string]any{"message": message}
		if conversation != "" {
			payload["conversation_id"] = conversation
		}
		response := api.request(t, http.MethodPost, "/api/v1/agent/chat", payload, tokens.AccessToken)
		assertStatus(t, response, http.StatusOK)
		if !strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
			t.Fatal("expected SSE")
		}
		scanner := bufio.NewScanner(response.Body)
		scanner.Buffer(make([]byte, 4096), 262144)
		var types []string
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var event struct {
				Type           string            `json:"type"`
				ConversationID string            `json:"conversation_id"`
				RunID          string            `json:"run_id"`
				Data           map[string]string `json:"data"`
			}
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
				t.Fatal(err)
			}
			if conversation == "" {
				conversation = event.ConversationID
			}
			if event.ConversationID != conversation || event.RunID == "" {
				t.Fatal("stream identity changed")
			}
			if event.Type == "token" && !strings.Contains(event.Data["text"], "演示回答") {
				t.Fatal("fake answer is not marked")
			}
			types = append(types, event.Type)
		}
		response.Body.Close()
		if scanner.Err() != nil || strings.Join(types, ",") != "thinking,token,done" {
			t.Fatalf("events=%v error=%v", types, scanner.Err())
		}
	}
	database, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var completed, messages int
	if err := database.QueryRow("SELECT COUNT(*) FROM agent_runs WHERE conversation_id=? AND user_id=? AND status='COMPLETED'", conversation, registered.User.ID).Scan(&completed); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow("SELECT COUNT(*) FROM agent_messages WHERE conversation_id=?", conversation).Scan(&messages); err != nil {
		t.Fatal(err)
	}
	if completed != 2 || messages != 4 {
		t.Fatalf("completed=%d messages=%d", completed, messages)
	}
}

func TestAgentGatewayBusinessToolStream(t *testing.T) {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("AGENT_BUSINESS_TOOLS_ENABLED")), "true") {
		t.Skip("set AGENT_BUSINESS_TOOLS_ENABLED=true to run the Agent business Tool integration test")
	}
	baseURL, dsn := os.Getenv("AGENT_INTEGRATION_BASE_URL"), os.Getenv("AGENT_TEST_MYSQL_DSN")
	if baseURL == "" || dsn == "" {
		t.Skip("set Agent integration configuration")
	}
	api := apiClient{baseURL: baseURL, client: &http.Client{Timeout: 10 * time.Second}}
	account := fmt.Sprintf("agenttool%d", time.Now().UnixNano())
	register := api.request(t, http.MethodPost, "/api/v1/auth/register", map[string]string{
		"username": account,
		"email":    account + "@example.com",
		"password": "correct-password-1",
	}, "")
	assertStatus(t, register, http.StatusOK)
	register.Body.Close()

	login := api.request(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"account":  account,
		"password": "correct-password-1",
	}, "")
	assertStatus(t, login, http.StatusOK)
	var tokens tokenPair
	decodeJSON(t, login.Body, &tokens)
	login.Body.Close()

	response := api.request(t, http.MethodPost, "/api/v1/agent/chat", map[string]any{
		"message": `/tool list_problems {"page":1,"page_size":1}`,
	}, tokens.AccessToken)
	assertStatus(t, response, http.StatusOK)
	defer response.Body.Close()
	if !strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatal("expected SSE")
	}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 4096), 262144)
	var tokenText string
	var types []string
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event struct {
			Type string            `json:"type"`
			Data map[string]string `json:"data"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
			t.Fatal(err)
		}
		types = append(types, event.Type)
		if event.Type == "token" {
			tokenText += event.Data["text"]
		}
	}
	if scanner.Err() != nil {
		t.Fatal(scanner.Err())
	}
	if strings.Join(types, ",") != "thinking,token,done" {
		t.Fatalf("events=%v", types)
	}
	if !strings.Contains(tokenText, "【演示工具结果】") || !strings.Contains(tokenText, `"source":"go-service"`) {
		t.Fatalf("Tool result was not returned through Agent SSE: %s", tokenText)
	}
}

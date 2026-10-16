package client

import (
	"fmt"
	"net/http"
	"regexp"

	"github.com/google/uuid"
)

const AgentJSONOperation = "HTTP Agent JSON"

var agentRAG = regexp.MustCompile(`^/api/v1/admin/agent/rag/documents/([0-9a-f-]{36})(?:/(enable|archive|retry|versions))?$`)
var agentRAGGroup = regexp.MustCompile(`^/api/v1/admin/agent/rag/knowledge-bases/([0-9a-f-]{36})(/rename)?$`)
var agentRAGEvidence = regexp.MustCompile(`^/api/v1/admin/agent/rag/runs/([0-9a-f-]{36})/evidence$`)
var agentEvalDataset = regexp.MustCompile(`^/api/v1/admin/agent/evals/datasets(?:/([0-9a-f-]{36})(/cases)?)?$`)

var agentResource = regexp.MustCompile(`^/api/v1/admin/agent/(agents|prompts|skills)(?:/([a-z][a-z0-9_]{1,63}))?(?:/(versions|archive|restore|disable|enable)(?:/([0-9a-f-]{36}))?)?$`)
var agentSkills = regexp.MustCompile(`^/api/v1/agent/agents/[a-z][a-z0-9_]{1,63}/skills$`)
var agentMessages = regexp.MustCompile(`^/api/v1/agent/conversations/([0-9a-f-]{36})/messages$`)
var agentModel = regexp.MustCompile(`^/api/v1/admin/agent/model-options/([0-9a-f-]{36})$`)
var agentVersion = regexp.MustCompile(`^/api/v1/admin/agent/(agents|prompts|skills)/versions/([0-9a-f-]{36})$`)

func AgentOperation(method, path string) (string, error) {
	allowed := false
	switch {
	case agentEvalDataset.MatchString(path):
		match := agentEvalDataset.FindStringSubmatch(path)
		if match[1] == "" {
			allowed = method == http.MethodGet || method == http.MethodPost
		} else {
			id, err := uuid.Parse(match[1])
			allowed = method == http.MethodGet && err == nil && id.String() == match[1]
		}
	case agentRAGEvidence.MatchString(path):
		match := agentRAGEvidence.FindStringSubmatch(path)
		id, err := uuid.Parse(match[1])
		allowed = method == http.MethodGet && err == nil && id.String() == match[1]
	case path == "/api/v1/admin/agent/rag/knowledge-bases" || path == "/api/v1/admin/agent/rag/documents":
		allowed = method == http.MethodGet || method == http.MethodPost
	case path == "/api/v1/admin/agent/rag/search" || path == "/api/v1/admin/agent/rag/preview":
		allowed = method == http.MethodPost
	case agentRAGGroup.MatchString(path):
		match := agentRAGGroup.FindStringSubmatch(path)
		id, err := uuid.Parse(match[1])
		expected := http.MethodGet
		if match[2] != "" {
			expected = http.MethodPost
		}
		allowed = method == expected && err == nil && id.String() == match[1]
	case agentRAG.MatchString(path):
		match := agentRAG.FindStringSubmatch(path)
		id, err := uuid.Parse(match[1])
		expected := http.MethodGet
		if match[2] != "" && match[2] != "versions" {
			expected = http.MethodPost
		}
		allowed = err == nil && id.String() == match[1] && method == expected
	case path == AgentChatPath:
		allowed = method == http.MethodPost
	case path == "/api/v1/agent/conversations" || path == "/api/v1/agent/agents" || path == "/api/v1/admin/agent/model-options" || path == "/api/v1/admin/agent/tools" || agentSkills.MatchString(path):
		allowed = method == http.MethodGet
	default:
		if conversation := agentMessages.FindStringSubmatch(path); conversation != nil {
			id, err := uuid.Parse(conversation[1])
			allowed = method == http.MethodGet && err == nil && id.String() == conversation[1]
		} else if version := agentVersion.FindStringSubmatch(path); version != nil {
			id, err := uuid.Parse(version[2])
			allowed = method == http.MethodGet && err == nil && id.String() == version[2]
		} else if model := agentModel.FindStringSubmatch(path); model != nil {
			id, err := uuid.Parse(model[1])
			allowed = method == http.MethodGet && err == nil && id.String() == model[1]
		} else if match := agentResource.FindStringSubmatch(path); match != nil {
			key, action, id := match[2], match[3], match[4]
			switch {
			case id != "":
				parsed, err := uuid.Parse(id)
				allowed = key != "" && action == "versions" && method == http.MethodGet && err == nil && parsed.String() == id
			case action != "":
				allowed = key != "" && ((action == "versions" && method == http.MethodGet) || (action != "versions" && method == http.MethodPost))
			case key != "":
				allowed = method == http.MethodGet || method == http.MethodPut
			default:
				allowed = method == http.MethodGet || method == http.MethodPost
			}
		}
	}
	if !allowed {
		return "", fmt.Errorf("unsupported Agent operation")
	}
	return "HTTP " + method + " " + path, nil
}

func AgentAdministrator(roles []string) bool {
	for _, role := range roles {
		if role == "admin" || role == "system_admin" || role == "agent_admin" {
			return true
		}
	}
	return false
}

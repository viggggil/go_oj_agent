package client

import (
	"fmt"
	"net/http"
	"regexp"

	"github.com/google/uuid"
)

const AgentJSONOperation = "HTTP Agent JSON"

var agentResource = regexp.MustCompile(`^/api/v1/admin/agent/(agents|prompts|skills)(?:/([a-z][a-z0-9_]{1,63}))?(?:/(versions|archive|restore|disable|enable)(?:/([0-9a-f-]{36}))?)?$`)
var agentSkills = regexp.MustCompile(`^/api/v1/agent/agents/[a-z][a-z0-9_]{1,63}/skills$`)

func AgentOperation(method, path string) (string, error) {
	allowed := false
	switch {
	case path == AgentChatPath:
		allowed = method == http.MethodPost
	case path == "/api/v1/agent/agents" || path == "/api/v1/admin/agent/model-options" || path == "/api/v1/admin/agent/tools" || agentSkills.MatchString(path):
		allowed = method == http.MethodGet
	default:
		if match := agentResource.FindStringSubmatch(path); match != nil {
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

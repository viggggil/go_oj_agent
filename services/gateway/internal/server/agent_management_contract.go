package server

import (
	"encoding/json"
	"strings"
)

// 按具体路由限制返回字段，普通目录不能返回管理正文或 Provider/凭据配置。
func validAgentJSONEnvelope(method, path string, payload map[string]json.RawMessage) bool {
	admin := strings.HasPrefix(path, "/api/v1/admin/")
	parts := strings.Split(path, "/")
	configuration := admin && (method != "GET" || (len(parts) != 6 && !(len(parts) == 8 && parts[7] == "versions")))
	if value, ok := payload["configuration"]; ok {
		return configuration && len(payload) == 1 && validAgentConfiguration(value, true, parts[5])
	}
	if configuration || len(payload) != 2 {
		return false
	}
	var items []json.RawMessage
	if json.Unmarshal(payload["items"], &items) != nil || items == nil {
		return false
	}
	skills := !admin && strings.HasSuffix(path, "/skills")
	if skills {
		var key string
		if json.Unmarshal(payload["default_skill_key"], &key) != nil || key == "" {
			return false
		}
	} else {
		var page struct {
			Page  int `json:"page"`
			Size  int `json:"page_size"`
			Total int `json:"total"`
		}
		if _, ok := agentJSONFields(payload["page"], "page page_size total", ""); !ok ||
			json.Unmarshal(payload["page"], &page) != nil || page.Page < 1 || page.Size < 1 || page.Size > 100 || page.Total < 0 || len(items) > page.Size {
			return false
		}
	}
	for _, item := range items {
		switch {
		case skills:
			if object, ok := agentJSONFields(item, "key name execution_mode default", ""); !ok || !agentJSONTypes(object, "key name execution_mode", "default", "") {
				return false
			}
		case !admin:
			if object, ok := agentJSONFields(item, "key name is_default", ""); !ok || !agentJSONTypes(object, "key name", "is_default", "") {
				return false
			}
		case strings.HasSuffix(path, "/model-options"):
			if object, ok := agentJSONFields(item, "id key model temperature verbosity max_output_tokens context_window_tokens", ""); !ok || !agentJSONTypes(object, "id key model", "", "max_output_tokens context_window_tokens") || !agentJSONNullable(object["temperature"], new(float64)) || !agentJSONNullable(object["verbosity"], new(string)) {
				return false
			}
		case strings.HasSuffix(path, "/tools"):
			object, ok := agentJSONFields(item, "name description read_scope allowed_roles side_effect sensitivity timeout_seconds requires_confirmation enabled input_schema output_schema", "")
			if !ok || !agentJSONTypes(object, "name description read_scope side_effect sensitivity", "requires_confirmation enabled", "") || !agentJSONStrings(object["allowed_roles"]) || !agentJSONRequired(object["timeout_seconds"], new(float64)) || !agentJSONObject(object["input_schema"]) || !agentJSONObject(object["output_schema"]) {
				return false
			}
		default:
			if !validAgentConfiguration(item, false, parts[5]) {
				return false
			}
		}
	}
	return true
}

func agentJSONFields(value json.RawMessage, required, optional string) (map[string]json.RawMessage, bool) {
	var object map[string]json.RawMessage
	if json.Unmarshal(value, &object) != nil || object == nil {
		return nil, false
	}
	allowed := make(map[string]bool)
	for _, key := range strings.Fields(required) {
		if _, ok := object[key]; !ok {
			return nil, false
		}
		allowed[key] = true
	}
	for _, key := range strings.Fields(optional) {
		allowed[key] = true
	}
	for key := range object {
		if !allowed[key] {
			return nil, false
		}
	}
	return object, true
}

func validAgentConfiguration(value json.RawMessage, full bool, resource string) bool {
	required := "id kind key archived disabled created_at created_by archived_at archived_by"
	optional := "name visibility is_test test_expires_at model_profile_id"
	if full {
		required += " content"
		optional = ""
	}
	object, ok := agentJSONFields(value, required, optional)
	if !ok {
		return false
	}
	var kind string
	if json.Unmarshal(object["kind"], &kind) != nil {
		return false
	}
	if (kind != "agent" && kind != "prompt" && kind != "skill") || kind+"s" != resource || !agentJSONTypes(object, "id kind key created_at", "archived disabled", "created_by") || !agentJSONNullable(object["archived_at"], new(string)) || !agentJSONNullable(object["archived_by"], new(int64)) {
		return false
	}
	if !full {
		switch kind {
		case "prompt":
			return len(object) == 9
		case "skill":
			return len(object) == 10 && agentJSONTypes(object, "name", "", "")
		default:
			return len(object) == 14 && agentJSONTypes(object, "name visibility model_profile_id", "is_test", "") && agentJSONNullable(object["test_expires_at"], new(string))
		}
	}
	fields := map[string]string{
		"prompt": "text variables",
		"skill":  "name prompt_id execution_mode allowed_tools budget",
		"agent":  "name prompt_id skill_ids default_skill_id model_profile_id allowed_tools budget visibility is_test test_expires_at knowledge_scope",
	}
	content, ok := agentJSONFields(object["content"], fields[kind], "")
	if !ok {
		return false
	}
	switch kind {
	case "prompt":
		if !agentJSONTypes(content, "text", "", "") || !agentJSONStrings(content["variables"]) {
			return false
		}
	case "skill":
		if !agentJSONTypes(content, "name prompt_id execution_mode", "", "") || !agentJSONStrings(content["allowed_tools"]) {
			return false
		}
	case "agent":
		if !agentJSONTypes(content, "name prompt_id default_skill_id model_profile_id visibility", "is_test", "") || !agentJSONNullable(content["test_expires_at"], new(string)) || !agentJSONStrings(content["skill_ids"]) || !agentJSONStrings(content["allowed_tools"]) || !agentJSONStrings(content["knowledge_scope"]) {
			return false
		}
	}
	if budget, exists := content["budget"]; exists {
		var limits map[string]json.RawMessage
		limits, ok = agentJSONFields(budget, "max_run_seconds max_output_chars max_events max_tool_calls max_model_calls max_input_tokens max_output_tokens", "")
		ok = ok && agentJSONTypes(limits, "", "", "max_output_chars max_events max_tool_calls max_model_calls max_input_tokens max_output_tokens") && agentJSONNullable(limits["max_run_seconds"], new(float64)) && string(limits["max_run_seconds"]) != "null"
	}
	return ok
}

func agentJSONTypes(object map[string]json.RawMessage, textFields, boolFields, intFields string) bool {
	for _, key := range strings.Fields(textFields) {
		if !agentJSONRequired(object[key], new(string)) {
			return false
		}
	}
	for _, key := range strings.Fields(boolFields) {
		if !agentJSONRequired(object[key], new(bool)) {
			return false
		}
	}
	for _, key := range strings.Fields(intFields) {
		if !agentJSONRequired(object[key], new(int64)) {
			return false
		}
	}
	return true
}

func agentJSONRequired(value json.RawMessage, target any) bool {
	return len(value) > 0 && string(value) != "null" && json.Unmarshal(value, target) == nil
}

func agentJSONNullable(value json.RawMessage, target any) bool {
	return len(value) > 0 && json.Unmarshal(value, target) == nil
}

func agentJSONStrings(value json.RawMessage) bool {
	var values []json.RawMessage
	if !agentJSONRequired(value, &values) || values == nil {
		return false
	}
	for _, item := range values {
		if !agentJSONRequired(item, new(string)) {
			return false
		}
	}
	return true
}

func agentJSONObject(value json.RawMessage) bool {
	var object map[string]json.RawMessage
	return agentJSONRequired(value, &object) && object != nil
}

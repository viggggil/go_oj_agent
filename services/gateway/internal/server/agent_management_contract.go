package server

import (
	"encoding/json"
	"strings"
)

// 按具体路由限制返回字段，普通目录不能返回管理正文或 Provider/凭据配置。
func validAgentJSONEnvelope(method, path string, payload map[string]json.RawMessage) bool {
	if strings.HasPrefix(path, "/api/v1/admin/agent/evals/") {
		return validEvalEnvelope(method, path, payload)
	}
	if strings.HasPrefix(path, "/api/v1/admin/agent/rag/") {
		return validRAGEnvelope(path, payload)
	}
	if strings.HasPrefix(path, "/api/v1/agent/conversations") {
		return validAgentHistory(path, payload)
	}
	if strings.HasPrefix(path, "/api/v1/admin/agent/model-options/") {
		object, ok := agentJSONFields(payload["model_option"], "id key model temperature verbosity max_output_tokens context_window_tokens archived disabled available", "")
		return len(payload) == 1 && ok && agentJSONTypes(object, "id key model", "archived disabled available", "max_output_tokens context_window_tokens") && agentJSONNullable(object["temperature"], new(float64)) && agentJSONNullable(object["verbosity"], new(string))
	}
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
			if object, ok := agentJSONFields(item, "key name execution_mode default", "description"); !ok || !agentJSONTypes(object, "key name execution_mode", "default", "") {
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

func validEvalEnvelope(method, path string, payload map[string]json.RawMessage) bool {
	if strings.HasSuffix(path, "/review-agent") {
		return len(payload) == 2 && validEvalModelReviewAttempt(payload["attempt"]) && agentJSONRequired(payload["duplicate"], new(bool))
	}
	if strings.HasSuffix(path, "/model-reviews") {
		return validEvalModelReviewsPage(payload)
	}
	if strings.HasSuffix(path, "/review") {
		return len(payload) == 3 && validEvalManualReview(payload["review"], true) && agentJSONRequired(payload["review_version"], new(int64)) && agentJSONRequired(payload["duplicate"], new(bool))
	}
	if strings.HasSuffix(path, "/reviews") {
		return validEvalReviewsPage(payload)
	}
	if strings.Contains(path, "/runs") {
		if strings.Contains(path, "/results/") {
			return len(payload) == 1 && validEvalResult(payload["result"], true)
		}
		if strings.HasSuffix(path, "/results") {
			return validEvalResultsPage(payload)
		}
		if method == "POST" && strings.HasSuffix(path, "/runs") {
			return len(payload) == 2 && validEvalRun(payload["run"]) && agentJSONRequired(payload["duplicate"], new(bool))
		}
		if strings.HasSuffix(path, "/cancel") {
			return len(payload) == 1 && validEvalRun(payload["run"])
		}
		if strings.Count(path, "/") == 7 {
			return len(payload) == 2 && validEvalRun(payload["run"]) && validEvalMetrics(payload["metrics"])
		}
		if strings.HasSuffix(path, "/runs") {
			return validEvalRunsPage(payload)
		}
		return false
	}
	if method == "POST" {
		if len(payload) != 2 {
			return false
		}
		item, ok := agentJSONFields(payload["dataset"], "id name description format_version content_hash case_count created_by created_at status", "duplicate")
		return ok && agentJSONTypes(item, "id name description format_version content_hash created_at status", "", "case_count created_by") && agentJSONRequired(payload["duplicate"], new(bool))
	}
	if strings.HasSuffix(path, "/datasets") {
		if len(payload) != 2 {
			return false
		}
		var items []json.RawMessage
		if json.Unmarshal(payload["items"], &items) != nil || items == nil {
			return false
		}
		page, ok := agentJSONFields(payload["page"], "page page_size total", "")
		if !ok || !agentJSONTypes(page, "", "", "page page_size total") {
			return false
		}
		for _, value := range items {
			item, valid := agentJSONFields(value, "id name description format_version content_hash case_count created_by created_at status", "")
			if !valid || !agentJSONTypes(item, "id name description format_version content_hash created_at status", "", "case_count created_by") {
				return false
			}
		}
		return true
	}
	if strings.HasSuffix(path, "/cases") {
		if len(payload) != 2 {
			return false
		}
		var values []json.RawMessage
		if json.Unmarshal(payload["items"], &values) != nil || values == nil {
			return false
		}
		page, ok := agentJSONFields(payload["page"], "page page_size total", "")
		if !ok || !agentJSONTypes(page, "", "", "page page_size total") {
			return false
		}
		for _, value := range values {
			caseValue, valid := agentJSONFields(value, "id category title case", "")
			if !valid || !agentJSONTypes(caseValue, "id category title", "", "") || !agentJSONObject(caseValue["case"]) {
				return false
			}
		}
		return true
	}
	if len(payload) == 1 {
		item, ok := agentJSONFields(payload["dataset"], "id name description format_version content_hash case_count created_by created_at status", "")
		return ok && agentJSONTypes(item, "id name description format_version content_hash created_at status", "", "case_count created_by")
	}
	if len(payload) != 2 {
		return false
	}
	item, ok := agentJSONFields(payload["dataset"], "id name description format_version content_hash case_count created_by created_at status", "")
	if !ok || !agentJSONTypes(item, "id name description format_version content_hash created_at status", "", "case_count created_by") {
		return false
	}
	var values []json.RawMessage
	if json.Unmarshal(payload["cases"], &values) != nil || values == nil {
		return false
	}
	for _, value := range values {
		caseValue, valid := agentJSONFields(value, "id category title case", "")
		if !valid || !agentJSONTypes(caseValue, "id category title", "", "") || !agentJSONObject(caseValue["case"]) {
			return false
		}
	}
	return true
}

func validEvalRun(value json.RawMessage) bool {
	object, ok := agentJSONFields(value, "id dataset_id agent_key mode repeat_count concurrency status total_cases completed_cases hard_failures error_count config_snapshot created_by request_id cancel_requested created_at version reviewer_agent_key reviewer_snapshot", "started_at finished_at")
	if !ok || !agentJSONTypes(object, "id dataset_id agent_key mode status request_id created_at", "cancel_requested", "repeat_count concurrency total_cases completed_cases hard_failures error_count created_by version") || !agentJSONNullable(object["reviewer_agent_key"], new(string)) || !agentJSONNullable(object["reviewer_snapshot"], &map[string]any{}) || !agentJSONObject(object["config_snapshot"]) || !agentJSONNullable(object["started_at"], new(string)) || !agentJSONNullable(object["finished_at"], new(string)) {
		return false
	}
	return true
}

func validEvalRunsPage(payload map[string]json.RawMessage) bool {
	if len(payload) != 2 {
		return false
	}
	var items []json.RawMessage
	if json.Unmarshal(payload["items"], &items) != nil || items == nil || len(items) > 100 {
		return false
	}
	page, ok := agentJSONFields(payload["page"], "page page_size total", "")
	if !ok || !agentJSONTypes(page, "", "", "page page_size total") {
		return false
	}
	for _, item := range items {
		if !validEvalRun(item) {
			return false
		}
	}
	return true
}

func validEvalResultsPage(payload map[string]json.RawMessage) bool {
	if len(payload) != 2 {
		return false
	}
	var items []json.RawMessage
	if json.Unmarshal(payload["items"], &items) != nil || items == nil || len(items) > 100 {
		return false
	}
	page, ok := agentJSONFields(payload["page"], "page page_size total", "")
	if !ok || !agentJSONTypes(page, "", "", "page page_size total") {
		return false
	}
	for _, value := range items {
		if !validEvalResult(value, false) {
			return false
		}
	}
	return true
}

func validEvalResult(value json.RawMessage, detail bool) bool {
	fields := "id run_id case_id attempt status checks created_at error_code latency_ms finished_at review_version manual_review effective_pass category title model_review_status model_review_attempt model_review model_review_error_code"
	if detail {
		fields += " answer model_summary case"
	} else {
		fields += " answer_preview"
	}
	object, valid := agentJSONFields(value, fields, "")
	if !valid || !agentJSONTypes(object, "id run_id case_id status created_at category title model_review_status", "effective_pass", "attempt review_version model_review_attempt") || !agentJSONObject(object["checks"]) || !agentJSONNullable(object["error_code"], new(string)) || !agentJSONNullable(object["latency_ms"], new(int64)) || !agentJSONNullable(object["finished_at"], new(string)) || !agentJSONNullable(object["model_review_error_code"], new(string)) {
		return false
	}
	if string(object["model_review"]) != "null" && !validEvalModelReview(object["model_review"], !detail) {
		return false
	}
	if string(object["manual_review"]) != "null" && !validEvalManualReview(object["manual_review"], detail) {
		return false
	}
	if detail {
		if !agentJSONObject(object["case"]) {
			return false
		}
		return agentJSONNullable(object["answer"], new(string)) && agentJSONNullable(object["model_summary"], &map[string]any{})
	}
	return agentJSONRequired(object["answer_preview"], new(string))
}

func validAgentHistory(path string, payload map[string]json.RawMessage) bool {
	var items []json.RawMessage
	if json.Unmarshal(payload["items"], &items) != nil || items == nil {
		return false
	}
	if strings.HasSuffix(path, "/messages") {
		conversation, ok := agentJSONFields(payload["conversation"], "id agent_key title", "")
		if len(payload) != 3 || !ok || !agentJSONTypes(conversation, "id title", "", "") ||
			!agentJSONNullable(conversation["agent_key"], new(string)) ||
			!agentJSONNullable(payload["next_before"], new(int64)) || len(items) > 4 {
			return false
		}
		for _, item := range items {
			object, ok := agentJSONFields(item, "id role content status error_code", "")
			if !ok || !agentJSONTypes(object, "role content", "", "id") ||
				!agentJSONNullable(object["status"], new(string)) || !agentJSONNullable(object["error_code"], new(string)) {
				return false
			}
			var role string
			_ = json.Unmarshal(object["role"], &role)
			if role != "user" && role != "assistant" {
				return false
			}
		}
		return true
	}
	page, ok := agentJSONFields(payload["page"], "page page_size total", "")
	if len(payload) != 2 || !ok || !agentJSONTypes(page, "", "", "page page_size total") || len(items) > 20 {
		return false
	}
	for _, item := range items {
		object, ok := agentJSONFields(item, "id agent_key title updated_at", "")
		if !ok || !agentJSONTypes(object, "id title updated_at", "", "") || !agentJSONNullable(object["agent_key"], new(string)) {
			return false
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
			return len(object) == 9 || (len(object) == 10 && agentJSONTypes(object, "name", "", ""))
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
	optionalContent := map[string]string{
		"prompt": "name",
		"skill":  "description text variables",
	}
	content, ok := agentJSONFields(object["content"], fields[kind], optionalContent[kind])
	if !ok {
		return false
	}
	switch kind {
	case "prompt":
		if name, exists := content["name"]; exists && !agentJSONRequired(name, new(string)) {
			return false
		}
		if !agentJSONTypes(content, "text", "", "") || !agentJSONStrings(content["variables"]) {
			return false
		}
	case "skill":
		if !agentJSONTypes(content, "name execution_mode", "", "") || !agentJSONStrings(content["allowed_tools"]) || !agentJSONNullable(content["prompt_id"], new(string)) {
			return false
		}
		if text, exists := content["text"]; exists && !agentJSONNullable(text, new(string)) {
			return false
		}
		if description, exists := content["description"]; exists && !agentJSONRequired(description, new(string)) {
			return false
		}
		if variables, exists := content["variables"]; exists && string(variables) != "null" && !agentJSONStrings(variables) {
			return false
		}
	case "agent":
		if !agentJSONTypes(content, "name prompt_id default_skill_id model_profile_id visibility", "is_test", "") || !agentJSONNullable(content["test_expires_at"], new(string)) || !agentJSONStrings(content["skill_ids"]) || !agentJSONStrings(content["allowed_tools"]) || !agentJSONStrings(content["knowledge_scope"]) {
			return false
		}
	}
	if budget, exists := content["budget"]; exists {
		if kind == "skill" && string(budget) == "null" {
			return ok
		}
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

func validEvalMetrics(value json.RawMessage) bool {
	object, ok := agentJSONFields(value, "recorded_cases check_passes effective_passes reviewed_cases average_latency_ms model_reviewed_cases model_review_unavailable model_passes", "")
	return ok && agentJSONTypes(object, "", "", "recorded_cases check_passes effective_passes reviewed_cases model_reviewed_cases model_review_unavailable model_passes") && agentJSONNullable(object["average_latency_ms"], new(int64))
}

func validEvalManualReview(value json.RawMessage, full bool) bool {
	fields := "pass"
	if full {
		fields += " scores tags note reviewed_by reviewed_at"
	}
	object, ok := agentJSONFields(value, fields, "")
	if !ok || !agentJSONRequired(object["pass"], new(bool)) {
		return false
	}
	if !full {
		return true
	}
	if !agentJSONTypes(object, "note reviewed_at", "", "reviewed_by") {
		return false
	}
	scores, ok := agentJSONFields(object["scores"], "task_completion technical_correctness evidence_support instruction_following clarity", "")
	if !ok {
		return false
	}
	for _, value := range scores {
		var score int
		if !agentJSONRequired(value, &score) || score < 0 || score > 4 {
			return false
		}
	}
	var tags []string
	if json.Unmarshal(object["tags"], &tags) != nil || tags == nil || len(tags) > 10 {
		return false
	}
	return true
}

func validEvalModelReview(value json.RawMessage, compact bool) bool {
	fields := "case_id pass scores hard_fail violations missing_facts reason"
	if compact {
		fields = "pass hard_fail"
	}
	object, ok := agentJSONFields(value, fields, "")
	if !ok || !agentJSONRequired(object["pass"], new(bool)) || !agentJSONRequired(object["hard_fail"], new(bool)) {
		return false
	}
	if compact {
		return true
	}
	if !agentJSONTypes(object, "case_id reason", "", "") {
		return false
	}
	scores, ok := agentJSONFields(object["scores"], "task_completion technical_correctness evidence_support instruction_following clarity", "")
	if !ok {
		return false
	}
	for _, value := range scores {
		var score int
		if !agentJSONRequired(value, &score) || score < 0 || score > 4 {
			return false
		}
	}
	for _, key := range []string{"violations", "missing_facts"} {
		var values []string
		if json.Unmarshal(object[key], &values) != nil || values == nil || len(values) > 64 {
			return false
		}
	}
	return true
}

func validEvalModelReviewAttempt(value json.RawMessage) bool {
	object, ok := agentJSONFields(value, "id result_id attempt requested_by status created_at review error_code finished_at", "")
	if !ok || !agentJSONTypes(object, "id result_id status created_at", "", "attempt requested_by") || !agentJSONNullable(object["error_code"], new(string)) || !agentJSONNullable(object["finished_at"], new(string)) {
		return false
	}
	if _, present := object["review"]; present {
		if string(object["review"]) != "null" && !validEvalModelReview(object["review"], false) {
			return false
		}
	}
	return true
}

func validEvalModelReviewsPage(payload map[string]json.RawMessage) bool {
	if len(payload) != 2 {
		return false
	}
	var items []json.RawMessage
	if json.Unmarshal(payload["items"], &items) != nil || items == nil || len(items) > 20 {
		return false
	}
	page, ok := agentJSONFields(payload["page"], "page page_size total", "")
	if !ok || !agentJSONTypes(page, "", "", "page page_size total") {
		return false
	}
	for _, item := range items {
		if !validEvalModelReviewAttempt(item) {
			return false
		}
	}
	return true
}

func validEvalReviewsPage(payload map[string]json.RawMessage) bool {
	if len(payload) != 2 {
		return false
	}
	var items []json.RawMessage
	if json.Unmarshal(payload["items"], &items) != nil || items == nil || len(items) > 20 {
		return false
	}
	page, ok := agentJSONFields(payload["page"], "page page_size total", "")
	if !ok || !agentJSONTypes(page, "", "", "page page_size total") {
		return false
	}
	for _, value := range items {
		object, ok := agentJSONFields(value, "id result_id version reviewed_by old_value new_value reason created_at", "")
		if !ok || !agentJSONTypes(object, "id result_id reason created_at", "", "version reviewed_by") || !validEvalManualReview(object["new_value"], true) {
			return false
		}
		if string(object["old_value"]) != "null" && !validEvalManualReview(object["old_value"], true) {
			return false
		}
	}
	return true
}

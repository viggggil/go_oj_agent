package server

import (
	"encoding/json"
	"strings"
)

func validRAGDocument(raw json.RawMessage) bool {
	object, ok := agentJSONFields(raw, "id document_id base_id enabled active_version title kind problem_id source_url tags filename content_hash size_bytes model_revision splitter_version created_by created_at state chunk_count error_code", "content")
	if !ok || !agentJSONTypes(object, "id document_id base_id title kind source_url filename content_hash model_revision splitter_version created_at state", "enabled", "size_bytes created_by chunk_count") || !agentJSONNullable(object["active_version"], new(string)) || !agentJSONNullable(object["problem_id"], new(int64)) || !agentJSONNullable(object["error_code"], new(string)) || !agentJSONStrings(object["tags"]) {
		return false
	}
	if content, exists := object["content"]; exists && !agentJSONRequired(content, new(string)) {
		return false
	}
	return true
}
func validRAGBase(raw json.RawMessage) bool {
	object, ok := agentJSONFields(raw, "id name created_by created_at", "")
	return ok && agentJSONTypes(object, "id name created_at", "", "created_by")
}
func validRAGEnvelope(path string, payload map[string]json.RawMessage) bool {
	if value, ok := payload["document"]; ok {
		return len(payload) == 1 && validRAGDocument(value)
	}
	if value, ok := payload["knowledge_base"]; ok {
		return len(payload) == 1 && validRAGBase(value)
	}
	if value, ok := payload["result"]; ok {
		fields, valid := agentJSONFields(value, "document_id version_id action", "")
		return len(payload) == 1 && valid && agentJSONTypes(fields, "document_id version_id action", "", "")
	}
	if value, ok := payload["matches"]; ok {
		var matches []json.RawMessage
		if len(payload) != 1 || !agentJSONRequired(value, &matches) || matches == nil || len(matches) > 10 {
			return false
		}
		for _, item := range matches {
			fields, valid := agentJSONFields(item, "score base_id document_id version_id title problem_id number text section start end", "")
			if !valid || !agentJSONTypes(fields, "base_id document_id version_id title text section", "", "number start end") || !agentJSONRequired(fields["score"], new(float64)) || !agentJSONNullable(fields["problem_id"], new(int64)) {
				return false
			}
		}
		return true
	}
	var items []json.RawMessage
	page, ok := agentJSONFields(payload["page"], "page page_size total", "")
	if len(payload) != 2 || !ok || !agentJSONTypes(page, "", "", "page page_size total") || !agentJSONRequired(payload["items"], &items) || items == nil || len(items) > 50 {
		return false
	}
	for _, item := range items {
		if strings.HasSuffix(path, "/knowledge-bases") {
			if !validRAGBase(item) {
				return false
			}
		} else if !validRAGDocument(item) {
			return false
		}
	}
	return true
}

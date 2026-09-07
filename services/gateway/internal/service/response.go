package service

import (
	"encoding/json"
	"net/http"
)

type Envelope struct {
	Data      any    `json:"data"`
	RequestID string `json:"request_id"`
}

type ErrorEnvelope struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

func WriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

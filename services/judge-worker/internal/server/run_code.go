package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/viggggil/go_oj_agent/services/judge-worker/internal/biz"
)

const (
	runCodePath       = "/internal/run-code"
	runCodeSourceMax  = 64 << 10
	runCodeInputMax   = 16 << 10
	runCodeOutputMax  = 16 << 10
	runCodeTimeLimit  = 2 * time.Second
	runCodeMemoryMax  = 128 << 20
	runCodeRequestMax = runCodeSourceMax + runCodeInputMax + 4096
)

type runCodeRequest struct {
	Language string `json:"language"`
	Source   string `json:"source"`
	Stdin    string `json:"stdin"`
}

type runCodeResponse struct {
	Status      string `json:"status"`
	Verdict     string `json:"verdict,omitempty"`
	Stdout      string `json:"stdout,omitempty"`
	Stderr      string `json:"stderr,omitempty"`
	TimeMS      int64  `json:"time_ms,omitempty"`
	MemoryBytes uint64 `json:"memory_bytes,omitempty"`
}

func NewRunCodeHandler(runners map[string]biz.LanguageRunner, token string) http.Handler {
	allowed := map[string]struct{}{"c": {}, "cpp": {}, "go": {}, "java": {}, "python": {}}
	capacity := make(chan struct{}, 1)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != runCodePath || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if provided == r.Header.Get("Authorization") || len(provided) != len(token) || subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			writeRunCodeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if r.Header.Get("Content-Encoding") != "" && r.Header.Get("Content-Encoding") != "identity" {
			writeRunCodeError(w, http.StatusUnsupportedMediaType, "unsupported media type")
			return
		}
		if r.Header.Get("Content-Type") != "application/json" {
			writeRunCodeError(w, http.StatusUnsupportedMediaType, "unsupported media type")
			return
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, runCodeRequestMax))
		decoder.DisallowUnknownFields()
		var request runCodeRequest
		if err := decoder.Decode(&request); err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				writeRunCodeError(w, http.StatusRequestEntityTooLarge, "request too large")
				return
			}
			writeRunCodeError(w, http.StatusBadRequest, "invalid request")
			return
		}
		if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
			writeRunCodeError(w, http.StatusBadRequest, "invalid request")
			return
		}
		source, input := []byte(request.Source), []byte(request.Stdin)
		_, supported := allowed[request.Language]
		runner := runners[request.Language]
		if !supported || runner == nil || len(source) == 0 || len(source) > runCodeSourceMax || len(input) > runCodeInputMax || !utf8.Valid(source) || !utf8.Valid(input) {
			writeRunCodeError(w, http.StatusBadRequest, "invalid request")
			return
		}
		select {
		case capacity <- struct{}{}:
			defer func() { <-capacity }()
		default:
			writeRunCodeError(w, http.StatusServiceUnavailable, "run-code capacity exhausted")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 70*time.Second)
		defer cancel()
		compiled, err := runner.Compile(ctx, source)
		if err != nil {
			writeRunCodeError(w, http.StatusServiceUnavailable, "sandbox unavailable")
			return
		}
		if compiled.Verdict != biz.VerdictAC || compiled.Artifact.ID == "" {
			writeRunCodeJSON(w, http.StatusOK, runCodeResponse{Status: "compile_error", Stderr: boundRunCodeText(compiled.Message)})
			return
		}
		defer func() {
			cleanup, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			defer cleanupCancel()
			if err := runner.DeleteArtifact(cleanup, compiled.Artifact); err != nil {
				slog.ErrorContext(cleanup, "run-code artifact cleanup failed", "error_code", "JUDGE_RUN_CODE_CLEANUP_FAILED", "language", request.Language, "error_type", fmt.Sprintf("%T", err))
			}
		}()
		result, err := runner.Run(ctx, compiled.Artifact, input, biz.ResourceLimits{TimeLimit: runCodeTimeLimit, MemoryBytes: runCodeMemoryMax})
		if err != nil {
			writeRunCodeError(w, http.StatusServiceUnavailable, "sandbox unavailable")
			return
		}
		writeRunCodeJSON(w, http.StatusOK, runCodeResponse{
			Status: "executed", Verdict: result.Verdict, Stdout: boundRunCodeText(string(result.Output)),
			Stderr: result.Message, TimeMS: result.Time.Milliseconds(), MemoryBytes: result.Memory,
		})
	})
}

func boundRunCodeText(value string) string {
	if len(value) > runCodeOutputMax {
		value = value[:runCodeOutputMax]
		for !utf8.ValidString(value) {
			value = value[:len(value)-1]
		}
	}
	return value
}

func writeRunCodeError(w http.ResponseWriter, status int, message string) {
	writeRunCodeJSON(w, status, map[string]string{"error": message})
}

func writeRunCodeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

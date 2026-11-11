package mcpserver

import (
	"bytes"
	"datascope-server/internal/audit"
	"datascope-server/internal/requesttiming"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTimingHandlerPreservesSSEAndRequestIdentity(t *testing.T) {
	t.Setenv("DATASCOPE_MCP_TIMING", "1")
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	var requestID string
	h := timingHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID = audit.From(r.Context()).RequestID
		requesttiming.Method(r.Context(), "ping")
		defer requesttiming.Start(r.Context(), "verify_token")()
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("event: message\ndata: {}\n\n"))
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Error(err)
		}
	}))
	req := httptest.NewRequest("POST", "/mcp/private-route", strings.NewReader("private-body"))
	req.Header.Set("Authorization", "Bearer private-token")
	out := httptest.NewRecorder()
	h.ServeHTTP(out, req)
	if out.Code != 200 || !out.Flushed || out.Body.String() != "event: message\ndata: {}\n\n" {
		t.Fatalf("SSE changed: %+v", out)
	}
	var record map[string]any
	if err := json.Unmarshal(logs.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if requestID == "" || record["request_id"] != requestID || record["method"] != "ping" || record["first_write_ms"] == nil {
		t.Fatalf("missing correlation/timing: %v", record)
	}
	if strings.Contains(logs.String(), "private-") {
		t.Fatal("request contents leaked")
	}
	logs.Reset()
	t.Setenv("DATASCOPE_MCP_TIMING", "0")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if logs.Len() != 0 {
		t.Fatal("disabled tracing logged")
	}
}

func TestTimedResponsePreservesFailureStatus(t *testing.T) {
	out := httptest.NewRecorder()
	w := &timedResponse{ResponseWriter: out}
	http.Error(w, "denied", http.StatusUnauthorized)
	w.WriteHeader(http.StatusOK)
	if out.Code != 401 || w.status != 401 || w.first.IsZero() {
		t.Fatal("failure status lost")
	}
}

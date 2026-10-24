package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

type responseStatus struct {
	http.ResponseWriter
	status int
}

func (w *responseStatus) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *responseStatus) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(p)
}
func (w *responseStatus) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *responseStatus) Flush() {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// HTTPHandler captures rejected MCP SQL before authentication, including revoked tokens.
func HTTPHandler(s *Store, source string, next http.Handler) http.Handler {
	if s == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			next.ServeHTTP(w, r)
			return
		}
		raw, e := io.ReadAll(io.LimitReader(r.Body, 1048577))
		if e != nil {
			http.Error(w, "invalid body", 400)
			return
		}
		if len(raw) > 1048576 {
			http.Error(w, "request too large", 413)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		var request struct {
			Method string `json:"method"`
			SQL    string `json:"sql"`
			Params struct {
				Name      string `json:"name"`
				Arguments struct {
					SQL string `json:"sql"`
				} `json:"arguments"`
			} `json:"params"`
		}
		_ = json.Unmarshal(raw, &request)
		meta := From(r.Context())
		meta.Source = source
		meta.Tool = request.Params.Name
		r = r.WithContext(With(r.Context(), meta))
		original := request.Params.Arguments.SQL
		if original == "" {
			original = request.SQL
		}
		kind := "http.request"
		if source != "web" {
			kind = "mcp.request"
		}
		event, e := s.Start(r.Context(), kind, original)
		if e != nil {
			http.Error(w, "audit unavailable", 503)
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), httpEventKey{}, event))
		status := &responseStatus{ResponseWriter: w}
		next.ServeHTTP(status, r)
		var failed error
		if status.status >= 400 {
			failed = fmt.Errorf("HTTP %d", status.status)
		}
		if e = s.Finish(event, failed); e != nil { // Response may already have been delivered; the running event remains durable.
			slog.Error("MCP audit completion failed", "error", e)
			return
		}
	})
}

type httpEventKey struct{}

func UpdateRequest(ctx context.Context, database string, actor int64) {
	if event, ok := ctx.Value(httpEventKey{}).(*Event); ok {
		if database != "" {
			event.DatabaseID = database
		}
		if actor > 0 {
			event.ActorID = actor
		}
	}
}

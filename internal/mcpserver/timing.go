package mcpserver

import (
	"datascope-server/internal/audit"
	"datascope-server/internal/requesttiming"
	"log/slog"
	"net/http"
	"os"
	"time"
)

// timingHandler wraps audit as well as routing and authentication. It never
// reads the request body or logs headers, SQL, tokens or business results.
func timingHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if os.Getenv("DATASCOPE_MCP_TIMING") != "1" || r.Method != http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		ctx := audit.With(r.Context(), audit.From(r.Context()))
		ctx, trace := requesttiming.New(ctx)
		out := &timedResponse{ResponseWriter: w}
		defer func() {
			elapsed := time.Since(start)
			method, stages, counts := trace.Snapshot()
			status := out.status
			if status == 0 {
				status = http.StatusOK
			}
			var first any
			if !out.first.IsZero() {
				first = float64(out.first.Sub(start)) / float64(time.Millisecond)
			}
			slog.InfoContext(ctx, "mcp.request.timing", "request_id", audit.From(ctx).RequestID,
				"method", method, "status", status, "total_ms", float64(elapsed)/float64(time.Millisecond),
				"first_write_ms", first, "stages_ms", stages, "stage_counts", counts)
		}()
		next.ServeHTTP(out, r.WithContext(ctx))
	})
}

type timedResponse struct {
	http.ResponseWriter
	first  time.Time
	status int
}

func (w *timedResponse) WriteHeader(status int) {
	if status >= 100 && status < 200 {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	if w.status != 0 {
		return
	}
	w.status = status
	w.first = time.Now()
	w.ResponseWriter.WriteHeader(status)
}
func (w *timedResponse) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}
func (w *timedResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *timedResponse) Flush()                      { _ = w.FlushError() }
func (w *timedResponse) FlushError() error {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}

package ai_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"datascope-server/internal/ai"
)

func TestCompletions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"ok", 200, `{"choices":[{"message":{"role":"assistant","content":"{\"terms\":[\"margin\"]}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":6,"completion_tokens":5,"total_tokens":11}}`, nil},
		{"broken envelope", 200, "{", ai.Invalid}, {"no choices", 200, "{}", ai.Invalid},
		{"broken JSON content", 200, `{"choices":[{"message":{"content":"oops"}}]}`, ai.Invalid},
		{"truncated", 200, `{"choices":[{"message":{"content":"{}"},"finish_reason":"length"}]}`, ai.Invalid},
		{"rate", 429, "sensitive upstream details", ai.RateLimited}, {"credentials", 403, "secret", ai.Unauthorized}, {"offline", 503, "", ai.Unavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Model    string
					Messages []ai.Message
					Format   map[string]string `json:"response_format"`
				}
				if json.NewDecoder(r.Body).Decode(&body) != nil || body.Model != "mock" || len(body.Messages) != 1 || body.Format["type"] != "json_object" || r.Header.Get("Authorization") != "Bearer test" {
					t.Error("incorrect wire request")
				}
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			}))
			defer server.Close()
			out, e := ai.New(server.URL, "mock", "test", time.Second).Complete(t.Context(), ai.Request{Messages: []ai.Message{{Role: "user", Content: "Extract terms as JSON"}}, JSONObject: true})
			if !errors.Is(e, tc.want) {
				t.Fatal(e)
			}
			if e == nil && (out.Usage == nil || out.Usage.Total != 11 || out.Text != `{"terms":["margin"]}`) {
				t.Fatal(out)
			}
		})
	}
}
func TestGatewayEndpoint(t *testing.T) {
	for _, tc := range []struct{ endpoint, want string }{
		{"/v1", "/v1/chat/completions"},
		{"/v1/", "/v1/chat/completions"},
		{"/gateway/v1?version=2", "/gateway/v1/chat/completions?version=2"},
		{"/v1/chat/completions", "/v1/chat/completions"},
		{"/custom/chat?version=2", "/custom/chat?version=2"},
	} {
		t.Run(tc.endpoint, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.RequestURI() != tc.want || r.Method != http.MethodPost {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.RequestURI())
				}
				http.Error(w, "private upstream detail", http.StatusNotFound)
			}))
			defer server.Close()
			_, err := ai.New(server.URL+tc.endpoint, "mock", "test", time.Second).Complete(t.Context(), ai.Request{})
			if !errors.Is(err, ai.Rejected) || !strings.Contains(err.Error(), "HTTP 404") || strings.Contains(err.Error(), "private") {
				t.Fatalf("unexpected diagnostic: %v", err)
			}
		})
	}
}

func TestCancellation(t *testing.T) {
	for _, test := range []string{"deadline", "parent deadline", "response body deadline", "cancel"} {
		t.Run(test, func(t *testing.T) {
			entered := make(chan struct{})
			release := make(chan struct{})
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if test == "response body deadline" {
					w.WriteHeader(200)
					w.(http.Flusher).Flush()
				}
				close(entered)
				select {
				case <-r.Context().Done():
				case <-release:
				}
			}))
			defer s.Close()
			defer close(release)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			timeout := 100 * time.Millisecond
			if test == "parent deadline" {
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(ctx, timeout)
				defer stop()
				timeout = time.Minute
			}
			want := context.DeadlineExceeded
			if test == "cancel" {
				timeout = time.Minute
				want = context.Canceled
				go func() {
					select {
					case <-entered:
						cancel()
					case <-ctx.Done():
					}
				}()
			}
			_, err := ai.New(s.URL, "mock", "", timeout).Complete(ctx, ai.Request{})
			if !errors.Is(err, want) {
				t.Fatal(err)
			}
			modelTimeout := test == "deadline" || test == "response body deadline"
			if strings.Contains(err.Error(), "DATASCOPE_AI_TIMEOUT") != modelTimeout {
				t.Fatalf("wrong timeout attribution: %v", err)
			}
		})
	}
}

func TestStreamingCompletion(t *testing.T) {
	const idle = 300 * time.Millisecond
	for _, mode := range []string{"progress", "stall", "truncated"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct{ Stream bool }
				if json.NewDecoder(r.Body).Decode(&req) != nil || !req.Stream {
					t.Error("stream not requested")
				}
				w.Header().Set("Content-Type", "text/event-stream")
				send := func(event string) { fmt.Fprintf(w, "data: %s\n\n", event); w.(http.Flusher).Flush() }
				send(`{"choices":[{"delta":{"content":"{"}}]}`)
				if mode == "truncated" {
					return
				}
				if mode == "stall" {
					<-r.Context().Done()
					return
				}
				for range 5 {
					time.Sleep(idle / 4)
					send(`{"choices":[{"delta":{"reasoning_content":"thinking"}}]}`)
				}
				send(`{"choices":[{"delta":{"content":"}"},"finish_reason":"stop"}]}`)
				send(`{"choices":[],"usage":{"total_tokens":11}}`)
				send("[DONE]")
			}))
			defer server.Close()
			out, err := ai.New(server.URL, "mock", "", idle).Complete(t.Context(), ai.Request{JSONObject: true})
			switch mode {
			case "progress":
				if err != nil || out.Text != "{}" || out.Usage == nil || out.Usage.Total != 11 {
					t.Fatal(out, err)
				}
			case "stall":
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal(err)
				}
			case "truncated":
				if !errors.Is(err, ai.Invalid) || out.Text != "" {
					t.Fatal(out, err)
				}
			}
		})
	}
}

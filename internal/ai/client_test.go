package ai_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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
func TestCancellation(t *testing.T) {
	for _, test := range []string{"deadline", "cancel"} {
		t.Run(test, func(t *testing.T) {
			entered := make(chan struct{})
			release := make(chan struct{})
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		})
	}
}

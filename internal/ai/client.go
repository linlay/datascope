// Package ai provides text and structured completions without domain-specific tools.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"datascope-server/internal/envflag"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type Request struct {
	Messages    []Message
	JSONObject  bool
	Temperature *float64
}
type Usage struct {
	Input  int `json:"prompt_tokens"`
	Output int `json:"completion_tokens"`
	Total  int `json:"total_tokens"`
}
type Response struct {
	Text  string
	Usage *Usage
}
type Client interface {
	Complete(context.Context, Request) (Response, error)
}

// Func adapts a deterministic completion function for offline callers and tests.
type Func func(context.Context, Request) (Response, error)

func (f Func) Complete(ctx context.Context, r Request) (Response, error) { return f(ctx, r) }

type Error string

func (e Error) Error() string { return string(e) }

const (
	Unavailable  Error = "AI service unavailable"
	Invalid      Error = "AI service returned an invalid response"
	Unauthorized Error = "AI provider rejected credentials"
	RateLimited  Error = "AI provider rate limit reached"
	Rejected     Error = "AI provider rejected request"
)

type ChatClient struct {
	endpoint, model, key string
	timeout              time.Duration
	http                 *http.Client
}

func New(endpoint, model, key string, timeout time.Duration) *ChatClient {
	return &ChatClient{endpoint, model, key, timeout, &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func FromEnv() Client {
	return New(os.Getenv("DATASCOPE_AI_ENDPOINT"), os.Getenv("DATASCOPE_AI_MODEL"), os.Getenv("DATASCOPE_AI_KEY"), envflag.Duration("DATASCOPE_AI_TIMEOUT", 120*time.Second))
}
func (c *ChatClient) Complete(parent context.Context, r Request) (out Response, err error) {
	start := time.Now()
	defer func() {
		status := "ok"
		if err != nil {
			status = err.Error()
		}
		slog.Info("model completion", "model", c.model, "duration_ms", time.Since(start).Milliseconds(), "status", status, "usage", out.Usage)
	}()
	u, e := url.Parse(c.endpoint)
	if e != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || c.model == "" || c.timeout <= 0 {
		return out, Unavailable
	}
	modelDeadline := fmt.Errorf("AI model idle timeout (%s, DATASCOPE_AI_TIMEOUT): %w", c.timeout, context.DeadlineExceeded)
	ctx, cancel := context.WithCancelCause(parent)
	defer cancel(nil)
	timer := time.AfterFunc(c.timeout, func() { cancel(modelDeadline) })
	defer timer.Stop()
	progress := func() {
		if timer.Stop() {
			timer.Reset(c.timeout)
		}
	}
	defer func() {
		if ctx.Err() != nil {
			out = Response{}
			err = ctx.Err()
			if context.Cause(ctx) == modelDeadline {
				err = modelDeadline
			}
		}
	}()
	body := map[string]any{"model": c.model, "messages": r.Messages, "stream": true, "stream_options": map[string]bool{"include_usage": true}}
	if r.JSONObject {
		body["response_format"] = map[string]string{"type": "json_object"}
	}
	if r.Temperature != nil {
		body["temperature"] = *r.Temperature
	}
	raw, e := json.Marshal(body)
	if e != nil {
		return out, Rejected
	}
	// A versioned API base is also commonly supplied by compatible gateways.
	// Keep explicit completion routes, path prefixes and query parameters intact.
	if path := strings.TrimRight(u.EscapedPath(), "/"); strings.HasSuffix(path, "/v1") {
		u = u.JoinPath("chat/completions")
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(raw))
	if e != nil {
		return out, Unavailable
	}
	req.Header.Set("Content-Type", "application/json")
	if c.key != "" {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	resp, e := c.http.Do(req)
	if e != nil {
		return out, Unavailable
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return out, fmt.Errorf("%w (HTTP %d): check provider key and model access", Unauthorized, resp.StatusCode)
	case resp.StatusCode == 429:
		return out, fmt.Errorf("%w (HTTP %d)", RateLimited, resp.StatusCode)
	case resp.StatusCode >= 500:
		return out, fmt.Errorf("%w (HTTP %d)", Unavailable, resp.StatusCode)
	case resp.StatusCode == http.StatusNotFound:
		return out, fmt.Errorf("%w (HTTP 404): check the chat completions endpoint and model name", Rejected)
	case resp.StatusCode != 200:
		return out, fmt.Errorf("%w (HTTP %d)", Rejected, resp.StatusCode)
	}
	if strings.EqualFold(strings.TrimSpace(strings.SplitN(resp.Header.Get("Content-Type"), ";", 2)[0]), "text/event-stream") {
		out, err = readStream(resp.Body, progress)
	} else {
		out, err = readCompletion(resp.Body)
	}
	if err != nil {
		return out, err
	}
	if r.JSONObject {
		var obj map[string]json.RawMessage
		if json.Unmarshal([]byte(out.Text), &obj) != nil || obj == nil {
			return out, Invalid
		}
	}

	return out, nil
}

func readCompletion(body io.Reader) (out Response, err error) {
	const limit = 1 << 20
	raw, e := io.ReadAll(io.LimitReader(body, limit+1))
	if e != nil {
		return out, Unavailable
	}
	var result struct {
		Choices []struct {
			Message Message
			Finish  string `json:"finish_reason"`
		}
		Usage *Usage
	}
	if len(raw) > limit || json.Unmarshal(raw, &result) != nil || len(result.Choices) != 1 {
		return out, Invalid
	}
	choice := result.Choices[0]
	out.Usage = result.Usage
	if strings.TrimSpace(choice.Message.Content) == "" || (choice.Finish != "" && choice.Finish != "stop") {
		return out, Invalid
	}
	out.Text = choice.Message.Content
	return out, nil
}

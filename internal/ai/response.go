package ai

import (
	"encoding/json"
	"fmt"
	"strings"
)

// InvalidResponse adds a bounded diagnostic, never raw provider content.
func InvalidResponse(reason string) error { return fmt.Errorf("%w: %s", Invalid, reason) }

// NormalizeJSONObject accepts a single object, optionally enclosed in a complete
// Markdown JSON fence. It never extracts JSON from prose or truncated output.
func NormalizeJSONObject(text string) (string, error) {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```json\n") || strings.HasPrefix(text, "```json\r\n") || strings.HasPrefix(text, "```\n") || strings.HasPrefix(text, "```\r\n") {
		if !strings.HasSuffix(text, "\n```") {
			return "", InvalidResponse("incomplete JSON code fence")
		}
		text = strings.TrimSpace(text[strings.IndexByte(text, '\n')+1 : len(text)-3])
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal([]byte(text), &obj) != nil || obj == nil {
		return "", InvalidResponse("expected one complete JSON object; check model JSON-output support")
	}
	return text, nil
}

func finishError(reason string) error {
	switch reason {
	case "length":
		return InvalidResponse("model output truncated (finish_reason=length); increase provider output token limit or shorten the request")
	case "content_filter":
		return InvalidResponse("provider filtered the response (finish_reason=content_filter)")
	case "tool_calls", "function_call":
		return InvalidResponse("provider returned tool calls instead of a text answer")
	default:
		return InvalidResponse("unsupported completion finish reason")
	}
}

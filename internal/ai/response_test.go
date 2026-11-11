package ai

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeJSONObject(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		valid      bool
	}{
		{"object", "  {\"script\":\"ok\"}  ", true},
		{"json_fence", "```json\n{\"script\":\"ok\"}\n```", true},
		{"plain_fence_crlf", "```\r\n{\"script\":\"ok\"}\r\n```", true},
		{"prose", "Answer: {}", false},
		{"missing_fence", "```json\n{}", false},
		{"truncated", "{\"script\":", false},
		{"array", "[]", false}, {"null", "null", false},
		{"two_objects", "{} {}", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value, err := NormalizeJSONObject(tc.text)
			if tc.valid {
				if err != nil || value != "{\"script\":\"ok\"}" {
					t.Fatal(value, err)
				}
			} else if !errors.Is(err, Invalid) {
				t.Fatal(err)
			}
		})
	}
}

func TestStreamCompletionBoundaries(t *testing.T) {
	content := "data: {\"choices\":[{\"delta\":{\"content\":\"{}\"}}]}\n\n"
	stop := "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}"
	for _, tc := range []struct{ name, body, reason string }{
		{"done", content + stop + "\n\ndata: [DONE]\n\n", ""},
		{"clean_eof", content + stop + "\n\n", ""},
		{"final_event_no_separator", content + stop, ""},
		{"done_no_separator", content + stop + "\n\ndata: [DONE]", ""},
		{"unfinished", content, "before finish_reason=stop"},
		{"premature_done", content + "data: [DONE]\n\n", "without finish_reason=stop"},
		{"empty", stop + "\n\n", "no answer content"},
		{"length", content + strings.Replace(stop, "stop", "length", 1) + "\n\n", "finish_reason=length"},
		{"bad_event", "data: secret malformed payload\n\n", "malformed stream event"},
		{"content_after_stop", content + stop + "\n\n" + content, "after stream completion"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := readStream(strings.NewReader(tc.body), func() {})
			if tc.reason == "" {
				if err != nil || out.Text != "{}" {
					t.Fatal(out, err)
				}
			} else if !errors.Is(err, Invalid) || !strings.Contains(err.Error(), tc.reason) || strings.Contains(err.Error(), "secret") || out.Text != "" {
				t.Fatal(out, err)
			}
		})
	}
}

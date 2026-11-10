package ai

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
)

// readStream assembles only the visible answer. Reasoning tokens also show
// that generation is progressing, but never become part of the JSON proposal.
func readStream(body io.Reader, progress func()) (Response, error) {
	const limit = 1 << 20
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 4096), limit)
	var event, answer strings.Builder
	var usage *Usage
	stopped := false
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			data, ok := strings.CutPrefix(line, "data:")
			if !ok {
				continue
			}
			if event.Len()+len(data)+1 > limit {
				return Response{}, Invalid
			}
			event.WriteString(strings.TrimPrefix(data, " "))
			event.WriteByte('\n')
			continue
		}
		if event.Len() == 0 {
			continue
		}
		data := strings.TrimSpace(event.String())
		event.Reset()
		if data == "[DONE]" {
			if !stopped || strings.TrimSpace(answer.String()) == "" {
				return Response{}, Invalid
			}
			return Response{Text: answer.String(), Usage: usage}, nil
		}
		var chunk struct {
			Error   json.RawMessage
			Usage   *Usage
			Choices []struct {
				Index  int
				Finish string `json:"finish_reason"`
				Delta  struct {
					Content   string
					Reasoning string `json:"reasoning_content"`
				}
			}
		}
		if json.Unmarshal([]byte(data), &chunk) != nil || len(chunk.Error) > 0 && string(chunk.Error) != "null" || len(chunk.Choices) > 1 {
			return Response{}, Invalid
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		if choice.Index != 0 || stopped || choice.Finish != "" && choice.Finish != "stop" || answer.Len()+len(choice.Delta.Content) > limit {
			return Response{}, Invalid
		}
		if choice.Delta.Content != "" || choice.Delta.Reasoning != "" || choice.Finish == "stop" {
			progress()
		}
		answer.WriteString(choice.Delta.Content)
		stopped = choice.Finish == "stop"
	}
	if scanner.Err() != nil {
		return Response{}, Unavailable
	}
	return Response{}, Invalid
}

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
	consume := func(data string) (bool, error) {
		if data == "[DONE]" {
			if !stopped {
				return false, InvalidResponse("stream ended without finish_reason=stop")
			}
			if strings.TrimSpace(answer.String()) == "" {
				return false, InvalidResponse("model returned no answer content")
			}
			return true, nil
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
			return false, InvalidResponse("malformed stream event or unexpected completion choices")
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
		if len(chunk.Choices) == 0 {
			return false, nil
		}
		choice := chunk.Choices[0]
		if choice.Finish != "" && choice.Finish != "stop" {
			return false, finishError(choice.Finish)
		}
		if choice.Index != 0 || stopped {
			return false, InvalidResponse("unexpected choice index or content after stream completion")
		}
		if answer.Len()+len(choice.Delta.Content) > limit {
			return false, InvalidResponse("model answer exceeds 1 MiB")
		}
		if choice.Delta.Content != "" || choice.Delta.Reasoning != "" || choice.Finish == "stop" {
			progress()
		}
		answer.WriteString(choice.Delta.Content)
		stopped = choice.Finish == "stop"
		return false, nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			data, ok := strings.CutPrefix(line, "data:")
			if !ok {
				continue
			}
			if event.Len()+len(data)+1 > limit {
				return Response{}, InvalidResponse("stream event exceeds 1 MiB")
			}
			event.WriteString(strings.TrimPrefix(data, " "))
			event.WriteByte('\n')
			continue
		}
		if event.Len() == 0 {
			continue
		}
		done, err := consume(strings.TrimSpace(event.String()))
		event.Reset()
		if err != nil {
			return Response{}, err
		}
		if done {
			return Response{Text: answer.String(), Usage: usage}, nil
		}
	}
	if scanner.Err() != nil {
		return Response{}, Unavailable
	}
	// Some gateways close cleanly after a stop event, or omit its trailing blank line.
	if event.Len() > 0 {
		if _, err := consume(strings.TrimSpace(event.String())); err != nil {
			return Response{}, err
		}
	}
	if !stopped {
		return Response{}, InvalidResponse("stream closed before finish_reason=stop; response may be truncated")
	}
	if strings.TrimSpace(answer.String()) == "" {
		return Response{}, InvalidResponse("model returned no answer content")
	}
	return Response{Text: answer.String(), Usage: usage}, nil
}

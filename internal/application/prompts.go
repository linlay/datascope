package application

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

type KnowledgePrompt struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
}
type KnowledgePrompts struct {
	Prompts    []KnowledgePrompt `json:"prompts"`
	NextCursor string            `json:"nextCursor,omitempty"`
}
type KnowledgePromptResult struct {
	Description string             `json:"description"`
	Messages    []KnowledgeMessage `json:"messages"`
}
type KnowledgeMessage struct {
	Role    string        `json:"role"`
	Content KnowledgeText `json:"content"`
}
type KnowledgeText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
type knowledgeCursor struct {
	Database string `json:"database"`
	Snapshot string `json:"snapshot"`
	After    string `json:"after"`
}

func (s *Service) ListKnowledgePrompts(ctx context.Context, in Input) (*KnowledgePrompts, error) {
	// List all authorized entries before pagination. Tool search's 50-result cap
	// must not silently hide knowledge from the standard prompts/list endpoint.
	value, err := s.readKnowledge(ctx, Input{DatabaseID: in.DatabaseID}, false, false, 0)
	if err != nil {
		return nil, err
	}
	entries := value.([]Knowledge)
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	raw, _ := json.Marshal(entries)
	snapshot := fmt.Sprintf("%x", sha256.Sum256(raw))
	start := 0
	if in.Cursor != "" {
		if len(in.Cursor) > 1024 {
			return nil, errors.New("无效知识分页游标")
		}
		raw, err := base64.RawURLEncoding.DecodeString(in.Cursor)
		if err != nil {
			return nil, errors.New("无效知识分页游标")
		}
		var cursor knowledgeCursor
		if json.Unmarshal(raw, &cursor) != nil || cursor.Database != in.DatabaseID || cursor.Snapshot != snapshot {
			return nil, errors.New("知识列表或权限已更新，请重新读取列表")
		}
		found := false
		for i, k := range entries {
			if k.ID == cursor.After {
				start = i + 1
				found = true
				break
			}
		}
		if !found {
			return nil, errors.New("无效知识分页游标")
		}
	}
	end := min(start+50, len(entries))
	out := &KnowledgePrompts{Prompts: []KnowledgePrompt{}}
	for _, k := range entries[start:end] {
		out.Prompts = append(out.Prompts, KnowledgePrompt{Name: k.ID, Title: k.Name, Description: k.Description})
	}
	if end < len(entries) {
		raw, _ := json.Marshal(knowledgeCursor{in.DatabaseID, snapshot, entries[end-1].ID})
		out.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	return out, nil
}
func (s *Service) GetKnowledgePrompt(ctx context.Context, in Input) (*KnowledgePromptResult, error) {
	if in.Name == "" || len(in.Arguments) != 0 {
		return nil, errors.New("需要知识 name；知识 Prompt 不接受模板参数")
	}
	value, err := s.ReadKnowledge(ctx, Input{DatabaseID: in.DatabaseID, ID: in.Name}, true, false)
	if err != nil {
		return nil, err
	}
	k := value.(Knowledge)
	return &KnowledgePromptResult{Description: k.Description, Messages: []KnowledgeMessage{{Role: "user", Content: KnowledgeText{Type: "text", Text: k.Content}}}}, nil
}

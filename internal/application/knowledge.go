package application

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// KnowledgeEntry deliberately excludes content, excerpts and management metadata.
type KnowledgeEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type KnowledgeSearch struct {
	Items      []KnowledgeEntry `json:"items"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

type knowledgeSearchCursor struct {
	Version int    `json:"version"`
	Scope   string `json:"scope"`
	Offset  int    `json:"offset"`
}

func (s *Service) SearchKnowledge(ctx context.Context, in Input) (*KnowledgeSearch, error) {
	keyword := strings.TrimSpace(in.Keyword)
	// Preserve management clients that previously submitted query.
	if keyword == "" {
		keyword = strings.TrimSpace(in.Query)
	}
	value, err := s.readKnowledge(ctx, Input{DatabaseID: in.DatabaseID, Query: keyword}, false, false, 0)
	if err != nil {
		return nil, err
	}
	entries := value.([]Knowledge)
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	// Bind pagination to the database, keyword and live authorized snapshot.
	// Revision is included so edits or unpublication require restarting the search.
	raw, err := json.Marshal(struct {
		Database, Keyword string
		Entries           []Knowledge
	}{in.DatabaseID, keyword, entries})
	if err != nil {
		return nil, err
	}
	scope := fmt.Sprintf("%x", sha256.Sum256(raw))
	start := 0
	if in.Cursor != "" {
		if len(in.Cursor) > 1024 {
			return nil, errors.New("无效知识搜索游标")
		}
		raw, err := base64.RawURLEncoding.DecodeString(in.Cursor)
		var cursor knowledgeSearchCursor
		if err != nil || json.Unmarshal(raw, &cursor) != nil || cursor.Version != 1 || cursor.Scope != scope || cursor.Offset <= 0 || cursor.Offset >= len(entries) {
			return nil, errors.New("知识搜索条件、列表或权限已更新，或游标无效，请重新检索")
		}
		start = cursor.Offset
	}
	end := min(start+50, len(entries))
	out := &KnowledgeSearch{Items: []KnowledgeEntry{}}
	for _, k := range entries[start:end] {
		out.Items = append(out.Items, KnowledgeEntry{ID: k.ID, Name: k.Name, Description: k.Description})
	}
	if end < len(entries) {
		raw, _ := json.Marshal(knowledgeSearchCursor{Version: 1, Scope: scope, Offset: end})
		out.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	return out, nil
}

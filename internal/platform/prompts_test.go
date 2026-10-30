package platform_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"datascope-server/internal/application"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestKnowledgeSwitchSavesWithoutOtherChanges(t *testing.T) {
	f := setup(t)
	d, err := f.c.Get("default")
	must(t, err)
	revision := d.Revision
	on := true
	d.KnowledgeEnabled = &on
	_, err = f.app.Call(f.ctx, "save_database", application.Input{DatabaseID: d.ID, Database: d})
	must(t, err)
	saved, err := f.c.Get(d.ID)
	must(t, err)
	if saved.KnowledgeEnabled == nil || !*saved.KnowledgeEnabled || saved.Revision != revision+1 {
		t.Fatal("knowledge-only change was skipped", saved)
	}
	_, err = f.app.Call(f.ctx, "save_database", application.Input{DatabaseID: d.ID, Database: saved})
	must(t, err)
	again, err := f.c.Get(d.ID)
	must(t, err)
	if again.Revision != saved.Revision {
		t.Fatal("unchanged save created another revision")
	}
}

func TestKnowledgePromptsPaginationAndLivePermissions(t *testing.T) {
	env := setupRealms(t)
	f := env.f
	ctx := context.Background()
	for i := 0; i < 53; i++ {
		description := fmt.Sprintf("Guide %d\n完整第二行\n第三行", i)
		_, err := f.app.Call(f.ctx, "save_knowledge", application.Input{DatabaseID: "default", ID: fmt.Sprintf("guide-%02d", i), Name: fmt.Sprintf("Guide %d", i), Description: &description, Content: "BODY_ONLY_ON_GET", Published: i != 52, Tables: []string{}})
		must(t, err)
	}
	tokens, _, _ := employeeTokens(t, env, "default", "employee-default-password")
	session := connectMCP(t, env.resource+"/default", tokens["access"])
	first, err := session.ListPrompts(ctx, nil)
	must(t, err)
	if len(first.Prompts) != 50 || first.NextCursor == "" {
		t.Fatal("pagination missing", len(first.Prompts))
	}
	if first.CacheScope != "private" || first.TTLMs != 0 {
		t.Fatal("knowledge cached publicly")
	}
	raw, _ := json.Marshal(first)
	if strings.Contains(string(raw), "BODY_ONLY_ON_GET") || !strings.Contains(string(raw), `完整第二行\n第三行`) {
		t.Fatal(string(raw))
	}
	second, err := session.ListPrompts(ctx, &mcp.ListPromptsParams{Cursor: first.NextCursor})
	must(t, err)
	if len(second.Prompts) != 2 || second.NextCursor != "" {
		t.Fatal("last page", second)
	}
	full, err := session.GetPrompt(ctx, &mcp.GetPromptParams{Name: "guide-00"})
	must(t, err)
	if len(full.Messages) != 1 || full.Messages[0].Role != "user" || full.Messages[0].Content.(*mcp.TextContent).Text != "BODY_ONLY_ON_GET" {
		t.Fatal(full)
	}
	for _, params := range []*mcp.GetPromptParams{{Name: "guide-52"}, {Name: "missing"}, {Name: "guide-00", Arguments: map[string]string{"template": "injected"}}} {
		if _, err = session.GetPrompt(ctx, params); err == nil {
			t.Fatal("draft, missing or parameterized prompt accepted", params)
		}
	}
	d, err := f.c.Get("default")
	must(t, err)
	off := false
	d.KnowledgeEnabled = &off
	must(t, f.c.Save(f.ctx, *d))
	list, err := session.ListPrompts(ctx, nil)
	must(t, err)
	if len(list.Prompts) != 0 {
		t.Fatal("disabled knowledge listed")
	}
	if _, err = session.GetPrompt(ctx, &mcp.GetPromptParams{Name: "guide-00"}); err == nil {
		t.Fatal("disabled knowledge readable")
	}
	on := true
	d, err = f.c.Get("default")
	must(t, err)
	d.KnowledgeEnabled = &on
	must(t, f.c.Save(f.ctx, *d))
	a := env.accounts["default"]
	a.Permissions = []string{"query"}
	_, err = f.m.SaveBusinessAccount(f.ctx, a, "", a.AuthVersion)
	must(t, err)
	if _, err = session.ListPrompts(ctx, nil); err == nil {
		t.Fatal("revoked knowledge permission retained")
	}
}

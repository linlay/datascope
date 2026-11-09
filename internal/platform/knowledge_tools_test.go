package platform_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"datascope-server/internal/application"
	"datascope-server/internal/management"
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

func TestKnowledgeOnlyAccountCanAuthorizeRefreshAndReadTools(t *testing.T) {
	env := setupRealms(t)
	f := env.f
	ctx := context.Background()
	a := env.accounts["default"]
	a.Permissions = []string{"knowledge.read"}
	a, err := f.m.SaveBusinessAccount(f.ctx, a, "", a.AuthVersion)
	must(t, err)
	_, err = f.app.Call(f.ctx, "save_knowledge", application.Input{DatabaseID: "default", ID: "guide", Name: "Guide", Content: "Knowledge without SQL access", Published: true})
	must(t, err)
	d, err := f.c.Get("default")
	must(t, err)
	off := false
	d.Config.Tables[0].Enabled = &off
	d.Config.Tables[0].Columns = []string{"id"}
	must(t, f.c.Save(f.ctx, *d))
	// Simulate a directly edited YAML generation with no query configuration.
	// Knowledge must remain available without opening a query service.
	must(t, f.m.UpdateRecords(f.ctx, "databases", "test.query-policy-unavailable", "default", func(records []management.Record) ([]management.Record, error) {
		for i, record := range records {
			if record.Key == "default" {
				var fields map[string]any
				if err := json.Unmarshal([]byte(record.Data), &fields); err != nil {
					return nil, err
				}
				fields["config"] = nil
				raw, err := json.Marshal(fields)
				if err != nil {
					return nil, err
				}
				records[i].Data = string(raw)
			}
		}
		return records, nil
	}))
	_, err = f.app.Call(f.ctx, "save_knowledge", application.Input{DatabaseID: "default", ID: "guide", Name: "Guide", Content: "Knowledge without SQL access", Published: true, Revision: 1})
	must(t, err)

	// Exercise the complete OAuth entry and refresh flow with no query/action grant.
	tokens, realm, _ := employeeTokens(t, env, "default", "employee-default-password")
	status, _, body := formRequest(t, http.DefaultClient, "POST", realm+"/oauth/token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tokens["refresh"]}, "client_id": {tokens["client"]}, "resource": {env.resource + "/default"}})
	if status != 200 {
		t.Fatalf("knowledge-only refresh %d: %s", status, body)
	}
	var refreshed struct {
		Access string `json:"access_token"`
	}
	must(t, json.Unmarshal([]byte(body), &refreshed))
	session := connectMCP(t, env.resource+"/default", refreshed.Access)
	list, err := knowledgeSearchMCP(t, session, map[string]any{})
	must(t, err)
	if len(list.Items) != 1 || list.Items[0].ID != "guide" {
		t.Fatal(list)
	}
	full, err := readKnowledgeMCP(t, session, "guide")
	must(t, err)
	if full.Content != "Knowledge without SQL access" {
		t.Fatal(full)
	}
	tools, err := session.ListTools(ctx, nil)
	must(t, err)
	if len(tools.Tools) != 2 {
		t.Fatal("knowledge grant exposed incorrect tools", tools)
	}
	for _, name := range []string{"query", "invoke_action"} {
		result, callErr := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: map[string]any{"sql": "SELECT * FROM items"}})
		if callErr == nil && !result.IsError {
			t.Fatalf("knowledge grant allowed %s", name)
		}
	}
	a.Permissions = []string{"query"}
	_, err = f.m.SaveBusinessAccount(f.ctx, a, "", a.AuthVersion)
	must(t, err)
	if _, err = readKnowledgeMCP(t, session, "guide"); err == nil {
		t.Fatal("revoked knowledge permission retained")
	}
}

// All helpers call the actual authenticated business HTTP MCP endpoint.
func knowledgeSearchMCP(t *testing.T, session *mcp.ClientSession, args map[string]any) (*application.KnowledgeSearch, error) {
	t.Helper()
	result, err := callMCP(t, session, "search_knowledge", args)
	if err != nil {
		return nil, err
	}
	if result.IsError {
		return nil, fmt.Errorf("knowledge search: %v", result.Content)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if strings.Contains(string(raw), "BODY_ONLY_ON_READ") {
		t.Fatal("search leaked body in MCP response", string(raw))
	}
	raw, err = json.Marshal(result.StructuredContent)
	if err != nil {
		return nil, err
	}
	var out application.KnowledgeSearch
	if err = json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	var wire struct {
		Items []map[string]any `json:"items"`
	}
	must(t, json.Unmarshal(raw, &wire))
	for _, item := range wire.Items {
		if len(item) != 3 || item["id"] == nil || item["name"] == nil || item["description"] == nil {
			t.Fatal("unexpected search fields", item)
		}
	}
	return &out, nil
}

func readKnowledgeMCP(t *testing.T, session *mcp.ClientSession, id string) (application.Knowledge, error) {
	t.Helper()
	result, err := callMCP(t, session, "read_knowledge", map[string]any{"id": id})
	if err != nil {
		return application.Knowledge{}, err
	}
	if result.IsError {
		return application.Knowledge{}, fmt.Errorf("knowledge read: %v", result.Content)
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		return application.Knowledge{}, err
	}
	var out application.Knowledge
	err = json.Unmarshal(raw, &out)
	return out, err
}

func TestKnowledgeToolsDisclosurePaginationAndLiveAccess(t *testing.T) {
	env := setupRealms(t)
	f := env.f
	ctx := context.Background()
	description := "活跃客户统计口径\n完整第二行\n第三行"
	for i := 0; i < 53; i++ {
		_, err := f.app.Call(f.ctx, "save_knowledge", application.Input{DatabaseID: "default", ID: fmt.Sprintf("guide-%02d", i), Name: "Guide", Description: &description, Content: "BODY_ONLY_ON_READ ActiveOnlyInBody", Published: i != 52})
		must(t, err)
	}
	_, err := f.app.Call(f.ctx, "save_knowledge", application.Input{DatabaseID: "second", ID: "only-other", Name: "Other private knowledge", Content: "BODY_ONLY_ON_READ", Published: true})
	must(t, err)
	tokens, _, _ := employeeTokens(t, env, "default", "employee-default-password")
	session := connectMCP(t, env.resource+"/default", tokens["access"])
	caps := session.InitializeResult().Capabilities
	if caps.Prompts != nil || caps.Resources == nil || caps.Tools == nil {
		t.Fatal("wrong initialize capabilities", caps)
	}
	tools, err := session.ListTools(ctx, nil)
	must(t, err)
	names := map[string]bool{}
	for _, tool := range tools.Tools {
		names[tool.Name] = true
		if tool.Name == "search_knowledge" || tool.Name == "read_knowledge" {
			if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
				t.Fatal("knowledge must be read-only", tool)
			}
		}
	}
	if !names["search_knowledge"] || !names["read_knowledge"] || names["list_knowledge"] {
		t.Fatal(names)
	}
	first, err := knowledgeSearchMCP(t, session, map[string]any{})
	must(t, err)
	if len(first.Items) != 50 || first.NextCursor == "" || first.Items[0].Description != description {
		t.Fatal(first)
	}
	second, err := knowledgeSearchMCP(t, session, map[string]any{"cursor": first.NextCursor})
	must(t, err)
	if len(second.Items) != 2 || second.NextCursor != "" || second.Items[0].ID == first.Items[0].ID {
		t.Fatal(second)
	}
	for _, keyword := range []string{"活跃客户", "activeonlyinbody", "   "} {
		page, err := knowledgeSearchMCP(t, session, map[string]any{"keyword": keyword})
		must(t, err)
		if len(page.Items) != 50 || page.NextCursor == "" {
			t.Fatal(keyword, page)
		}
	}
	page, err := knowledgeSearchMCP(t, session, map[string]any{"keyword": "no-such-term"})
	must(t, err)
	if len(page.Items) != 0 || page.NextCursor != "" {
		t.Fatal(page)
	}
	if _, err = knowledgeSearchMCP(t, session, map[string]any{"keyword": "different", "cursor": first.NextCursor}); err == nil {
		t.Fatal("cursor accepted different keyword")
	}
	if _, err = knowledgeSearchMCP(t, session, map[string]any{"cursor": "bad-cursor"}); err == nil {
		t.Fatal("invalid cursor accepted")
	}
	full, err := readKnowledgeMCP(t, session, "guide-00")
	must(t, err)
	if full.Content != "BODY_ONLY_ON_READ ActiveOnlyInBody" || full.Description != description || full.DatabaseID != "default" || !full.Published {
		t.Fatal(full)
	}
	for _, id := range []string{"guide-52", "missing", "only-other", "second/only-other", ""} {
		if _, err = readKnowledgeMCP(t, session, id); err == nil {
			t.Fatal("read inaccessible knowledge", id)
		}
	}
	// A previously found entry is not a capability: unpublish, then re-check.
	_, err = f.app.Call(f.ctx, "save_knowledge", application.Input{DatabaseID: "default", ID: "guide-00", Name: full.Name, Description: &description, Content: full.Content, Published: false, Revision: full.Revision})
	must(t, err)
	if _, err = readKnowledgeMCP(t, session, "guide-00"); err == nil {
		t.Fatal("unpublished entry readable")
	}
	if _, err = knowledgeSearchMCP(t, session, map[string]any{"cursor": first.NextCursor}); err == nil {
		t.Fatal("stale search cursor accepted")
	}
	d, err := f.c.Get("default")
	must(t, err)
	off := false
	d.KnowledgeEnabled = &off
	must(t, f.c.Save(f.ctx, *d))
	tools, err = session.ListTools(ctx, nil)
	must(t, err)
	for _, tool := range tools.Tools {
		if tool.Name == "search_knowledge" || tool.Name == "read_knowledge" {
			t.Fatal("disabled tool discovered", tool)
		}
	}
	if _, err = knowledgeSearchMCP(t, session, map[string]any{}); err == nil {
		t.Fatal("disabled search callable")
	}
	if _, err = readKnowledgeMCP(t, session, "guide-01"); err == nil {
		t.Fatal("disabled read callable")
	}
	if _, err = f.app.Call(f.readerContext(), "search_knowledge", application.Input{DatabaseID: "default"}); err == nil {
		t.Fatal("workbench ignored switch")
	}
	if _, err = f.app.Call(f.readerContext(), "read_knowledge", application.Input{DatabaseID: "default", ID: "guide-01"}); err == nil {
		t.Fatal("workbench read ignored switch")
	}
	// Management editing and draft listing remain usable while business knowledge is off.
	if _, err = f.app.Call(f.ctx, "get_knowledge", application.Input{DatabaseID: "default", ID: "guide-00"}); err != nil {
		t.Fatal(err)
	}
	on := true
	d, err = f.c.Get("default")
	must(t, err)
	d.KnowledgeEnabled = &on
	// Explicit knowledge_enabled works even with neither legacy tool in tools.
	d.Tools = []string{"query", "list_tables"}
	must(t, f.c.Save(f.ctx, *d))
	_, err = knowledgeSearchMCP(t, session, map[string]any{})
	must(t, err)
	a := env.accounts["default"]
	a.Permissions = []string{"query"}
	_, err = f.m.SaveBusinessAccount(f.ctx, a, "", a.AuthVersion)
	must(t, err)
	if _, err = readKnowledgeMCP(t, session, "guide-01"); err == nil {
		t.Fatal("revoked permission retained")
	}
	if _, err = knowledgeSearchMCP(t, session, map[string]any{}); err == nil {
		t.Fatal("unauthorized search allowed")
	}
	for _, action := range []string{"list_knowledge_prompts", "get_knowledge_prompt"} {
		if _, err = f.app.Call(f.ctx, action, application.Input{DatabaseID: "default", Name: "guide-01"}); err == nil {
			t.Fatal("retired prompt simulation accepted", action)
		}
	}
}

func TestKnowledgeSearchBeyondOldCaps(t *testing.T) {
	f := setup(t)
	// Drafts must not consume a limit before the published-entry filter.
	must(t, f.m.UpdateRecords(f.ctx, "knowledge", "test.bulk", "default", func(records []management.Record) ([]management.Record, error) {
		for i := 0; i < 1010; i++ {
			k := application.Knowledge{DatabaseID: "default", ID: fmt.Sprintf("bulk-%04d", i), Name: "Common", Description: "Complete description", Content: "BODY_ONLY_ON_READ", Published: i >= 505, Revision: 1}
			raw, err := json.Marshal(k)
			if err != nil {
				return nil, err
			}
			records = append(records, management.Record{Key: "default/" + k.ID, Data: string(raw)})
		}
		return records, nil
	}))
	cursor := ""
	seen := map[string]bool{}
	for {
		value, err := f.app.Call(f.readerContext(), "search_knowledge", application.Input{DatabaseID: "default", Keyword: "common", Cursor: cursor})
		must(t, err)
		page := value.(*application.KnowledgeSearch)
		for _, item := range page.Items {
			if seen[item.ID] {
				t.Fatal("duplicate page entry", item.ID)
			}
			seen[item.ID] = true
		}
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	if len(seen) != 505 {
		t.Fatal("silently truncated search", len(seen))
	}
	// All supplied words participate; no hidden 20-keyword cutoff.
	value, err := f.app.Call(f.readerContext(), "search_knowledge", application.Input{DatabaseID: "default", Keyword: strings.Repeat("common ", 20) + "unmatched"})
	must(t, err)
	if len(value.(*application.KnowledgeSearch).Items) != 0 {
		t.Fatal("keyword truncation")
	}
}

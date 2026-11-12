package platform_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"datascope-server/internal/action"
	"datascope-server/internal/ai"
	"datascope-server/internal/application"
	"datascope-server/internal/management"
)

func TestAssistantRollbackWorkflowAndModelReuse(t *testing.T) {
	f, business := setupAction(t, false)
	db, err := f.c.Get("default")
	must(t, err)
	saved := db.Actions[0]
	// The SQLite fixture owns item 1 as user 10 and item 2 as user 20.
	draft := action.Definition{Name: "rename_mine", Title: "Rename my item", Description: "Only the manager may rename it"}
	published := false
	draft.Published = &published
	input := application.Input{DatabaseID: db.ID, Revision: db.Revision, Assistant: &application.AssistantInput{Mode: "draft", Intent: "Rename items.name by id only when owner_id is the current user", Draft: &draft}}
	var diagnostic string
	f.app.Model = ai.Func(func(_ context.Context, r ai.Request) (ai.Response, error) {
		var result any
		prompt := r.Messages[0].Content
		data := r.Messages[1].Content
		switch {
		case strings.Contains(prompt, "Extract vocabulary"):
			result = map[string]any{"terms": []string{"margin"}}
		case strings.Contains(prompt, "actual trial summary"):
			diagnostic = data
			if strings.Contains(data, "business-password") || strings.Contains(data, "test-sensitive") || strings.Contains(data, "SELECT") || strings.Contains(data, "invisible") {
				t.Fatal("raw trial data leaked")
			}
			result = map[string]string{"explanation": "Inspect the affected rows or business guard.", "repair": "This must only be returned for a script defect."}
		case strings.Contains(prompt, "synthetic example"):
			result = map[string]any{"arguments": map[string]any{"id": 1, "name": "test-sensitive"}, "expected": "One owned row changes in the preview only"}
		default:
			if !strings.Contains(data, "items") || strings.Contains(data, "business-password") {
				t.Fatal("bad context")
			}
			result = map[string]any{"parameters": saved.Parameters, "script": saved.Script}
		}
		raw, e := json.Marshal(result)
		return ai.Response{Text: string(raw)}, e
	})
	terms, err := extractTerms(f.ctx, f.app.Model, "margin")
	must(t, err)
	if len(terms) != 1 || terms[0] != "margin" {
		t.Fatal(terms)
	}
	if _, err = f.app.Call(f.readerContext(), "assist_action", input); err == nil {
		t.Fatal("missing grants")
	}
	if _, err = f.app.Call(business, "assist_action", input); err == nil {
		t.Fatal("business session accepted")
	}
	out, err := f.app.Call(f.ctx, "assist_action", input)
	must(t, err)
	unchanged, err := f.c.Get(db.ID)
	must(t, err)
	if len(unchanged.Actions) != 1 || unchanged.Revision != db.Revision {
		t.Fatal("suggestion mutated config")
	}
	suggestion := out.(map[string]any)
	draft.Parameters = suggestion["parameters"].(map[string]action.Parameter)
	draft.Script = suggestion["script"].(string)
	db.Actions = append(db.Actions, draft)
	_, err = f.app.Call(f.ctx, "configure_actions", application.Input{DatabaseID: db.ID, Database: db})
	must(t, err)
	db, err = f.c.Get(db.ID)
	must(t, err)
	input.Name = draft.Name
	input.Revision = db.Revision
	input.Assistant = &application.AssistantInput{Mode: "test"}
	must(t, f.m.SaveGrant(f.ctx, f.admin.ID, management.Grant{UserID: f.reader.ID, DatabaseID: db.ID, Permissions: []string{"config.read", "action.write"}}))
	if _, err = f.app.Call(f.readerContext(), "assist_action", input); err == nil {
		t.Fatal("missing impersonation grant")
	}
	out, err = f.app.Call(f.ctx, "assist_action", input)
	must(t, err)
	input.Arguments = out.(map[string]any)["arguments"].(map[string]any)
	for _, tc := range []struct {
		identity            int64
		id, status, summary string
	}{{10, "allowed", "preview", "affected_rows"}, {20, "rejected", "denied", "business_rule_denied"}} {
		input.BusinessUserID = tc.identity
		input.RequestID = tc.id
		response, err := f.app.Call(f.ctx, "try_action", input)
		must(t, err)
		trial := response.(*application.ActionResponse)
		if trial.Status != tc.status {
			t.Fatal(trial)
		}
		if trial.Status == "preview" && (len(trial.Preview.Writes) != 1 || trial.Preview.Writes[0].Rows != 1 || trial.Preview.Writes[0].SQL == "" || len(trial.Preview.Writes[0].Binds) != 3) {
			t.Fatal(trial.Preview)
		}
		if trial.Status == "denied" && trial.Message != "invisible" {
			t.Fatal(trial.Message)
		}
		input.Assistant.Mode = "diagnose"
		out, err = f.app.Call(f.ctx, "assist_action", input)
		must(t, err)
		if !strings.Contains(diagnostic, tc.summary) || out.(application.ActionSuggestion).Repair != "" {
			t.Fatal(diagnostic)
		}
		rows, err := f.st.Query(business, "SELECT name FROM items WHERE id=1", 1)
		must(t, err)
		if rows.Rows[0]["name"] != "one" {
			t.Fatal("trial committed")
		}
	}
	input.RequestID = "invalid-arguments"
	input.Arguments = map[string]any{"unexpected": "test-sensitive"}
	if _, err = f.app.Call(f.ctx, "try_action", input); err == nil {
		t.Fatal("invalid arguments accepted")
	}
	_, err = f.app.Call(f.ctx, "assist_action", input)
	must(t, err)
	if !strings.Contains(diagnostic, "invalid_arguments") {
		t.Fatal(diagnostic)
	}
}

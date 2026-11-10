package platform_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"datascope-server/internal/action"
	"datascope-server/internal/ai"
	"datascope-server/internal/application"
)

func TestAssistantDraftValidation(t *testing.T) {
	f, _ := setupAction(t, false)
	db, err := f.c.Get("default")
	must(t, err)
	def := db.Actions[0]
	input := application.Input{DatabaseID: db.ID, Revision: db.Revision, Name: def.Name, Assistant: &application.AssistantInput{Mode: "draft", Draft: &def, Intent: "Use the current editor content"}}
	def.Script += "\n// unsaved edit"
	calls := 0
	var output string
	f.app.Model = ai.Func(func(_ context.Context, r ai.Request) (ai.Response, error) {
		calls++
		var data struct {
			Tables     map[string][]string
			Definition action.Definition
			Dialect    string
		}
		err := json.Unmarshal([]byte(r.Messages[1].Content), &data)
		if err != nil || len(data.Tables["items"]) != 4 || !strings.Contains(data.Definition.Script, "unsaved edit") || data.Dialect != "sqlite" {
			t.Fatal("incomplete authoring context", data, err)
		}
		return ai.Response{Text: output}, nil
	})
	output = `{"question":"Which ownership column should be used?","script":"unsafe()"}`
	out, err := f.app.Call(f.ctx, "assist_action", input)
	must(t, err)
	if out.(application.ActionSuggestion).Script != "" {
		t.Fatal("clarification exposed script")
	}
	for _, bad := range []string{`{}`, `{"script":"function run(args,ctx){","parameters":{}}`, `{"script":"function run(args, ctx){return db.one\u0060SELECT id FROM items\u0060}","parameters":{}}`, `{"script":"function run(args, ctx){return 1}","parameters":{"id":{"type":"object"}}}`} {
		output = bad
		if _, err = f.app.Call(f.ctx, "assist_action", input); err == nil {
			t.Fatal("invalid suggestion accepted")
		}
	}
	// Local enum literals and source code stay out of test-design prompts.
	stored := &db.Actions[0]
	stored.Parameters["name"] = action.Parameter{Type: "string", Enum: []any{"private-allowed-value"}}
	must(t, f.c.Save(f.ctx, *db))
	db, err = f.c.Get(db.ID)
	must(t, err)
	input.Revision = db.Revision
	input.Assistant.Mode = "test"
	f.app.Model = ai.Func(func(_ context.Context, r ai.Request) (ai.Response, error) {
		calls++
		wire := r.Messages[1].Content
		if strings.Contains(wire, "private-allowed-value") || strings.Contains(wire, "function run") || !strings.Contains(wire, "enum_choices") {
			t.Fatal("unsafe test-design context")
		}
		return ai.Response{Text: `{"arguments":{"name":0,"id":1},"expected":"one row"}`}, nil
	})
	out, err = f.app.Call(f.ctx, "assist_action", input)
	must(t, err)
	if out.(map[string]any)["arguments"].(map[string]any)["name"] != "private-allowed-value" {
		t.Fatal("enum index not resolved locally")
	}
	input.Assistant.Mode = "draft"
	before := calls
	input.Name = "other_database_action"
	if _, err = f.app.Call(f.ctx, "assist_action", input); err == nil {
		t.Fatal("unknown action accepted")
	}
	input.Name = def.Name
	input.Revision--
	if _, err = f.app.Call(f.ctx, "assist_action", input); err == nil {
		t.Fatal("stale config accepted")
	}
	if calls != before {
		t.Fatal("unauthorized context reached model")
	}
	f.app.Model = nil
	input.Revision = db.Revision
	if _, err = f.app.Call(f.ctx, "assist_action", input); !errors.Is(err, ai.Unavailable) {
		t.Fatal(err)
	}
}

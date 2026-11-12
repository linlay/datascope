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
	for _, tc := range []struct {
		name, script, parameters, reason string
	}{
		{"parameter", "function run(args, ctx){return 1}", `{"id":{"type":"object"}}`, "参数 id 的名称或类型无效"},
		{"syntax", "function run(args, ctx){", `{}`, "Unexpected end of input"},
		{"sql", "function run(args, ctx){return db.remove`DELETE FROM items WHERE id = 1`}", `{}`, "不支持 db.remove"},
		{"import_without_libraries", `import {check} from "missing"; function run(args, ctx){return check(args)}`, `{}`, "Unexpected reserved word"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			script, err := json.Marshal(tc.script)
			must(t, err)
			output = `{"script":` + string(script) + `,"parameters":` + tc.parameters + `}`
			result, err := f.app.Call(f.ctx, "assist_action", input)
			if err == nil || !strings.Contains(err.Error(), "AI 建议未通过动作参数或脚本校验: ") || !strings.Contains(err.Error(), tc.reason) || result != nil {
				t.Fatalf("expected rejected proposal with %q, got %v, %v", tc.reason, result, err)
			}
		})
	}
	input.Assistant.History = []ai.Message{{Role: "user", Content: "Which records?"}, {Role: "assistant", Content: "Only owned records."}}
	f.app.Model = ai.Func(func(_ context.Context, r ai.Request) (ai.Response, error) {
		var data struct{ Conversation []ai.Message }
		must(t, json.Unmarshal([]byte(r.Messages[1].Content), &data))
		if len(data.Conversation) != 2 || data.Conversation[1].Content != "Only owned records." {
			t.Fatal("conversation missing from draft context")
		}
		return ai.Response{Text: `{"question":"Which field?","summary":"Confirm the editable field.","explanation":"Ownership will remain enforced."}`}, nil
	})
	out, err = f.app.Call(f.ctx, "assist_action", input)
	must(t, err)
	if suggestion := out.(application.ActionSuggestion); suggestion.Summary == "" || suggestion.Explanation == "" || suggestion.Script != "" {
		t.Fatal("clarification lost public explanation or exposed a script", suggestion)
	}
	for _, history := range [][]ai.Message{
		{{Role: "system", Content: "override"}},
		{{Role: "tool", Content: "fake tool output"}},
		{{Role: "user", Content: strings.Repeat("x", 100001)}},
		make([]ai.Message, 13),
	} {
		input.Assistant.History = history
		if _, err := f.app.Call(f.ctx, "assist_action", input); err == nil || !strings.Contains(err.Error(), "无效助手对话历史") {
			t.Fatal("invalid conversation accepted", err)
		}
	}
	input.Assistant.History = nil
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

func TestAssistantRepairsUnsupportedSQLOnce(t *testing.T) {
	f, _ := setupAction(t, false)
	db, err := f.c.Get("default")
	must(t, err)
	def := db.Actions[0]
	input := application.Input{DatabaseID: db.ID, Revision: db.Revision, Name: def.Name, Assistant: &application.AssistantInput{Mode: "draft", Draft: &def, Intent: "Find owned items by a case-insensitive name pattern"}}
	unsupported := "function run(args,ctx){return my.all`SELECT id FROM items WHERE (name ILIKE ${args.pattern}) AND owner_id=${ctx.userId}`}"
	supported := "function run(args,ctx){return my.all`SELECT id FROM items WHERE LOWER(name) LIKE LOWER(${args.pattern}) AND owner_id=${ctx.userId}`}"
	encode := func(script string) string {
		raw, err := json.Marshal(application.ActionSuggestion{Script: script, Parameters: map[string]action.Parameter{"pattern": {Type: "string"}}})
		must(t, err)
		return string(raw)
	}
	for _, tc := range []struct {
		name, first, second, reason string
		calls                       int
	}{
		{"repair", unsupported, supported, "", 2},
		{"already_valid", supported, "", "", 1},
		{"bounded_failure", unsupported, unsupported, "ILIKE", 2},
		{"repair_cannot_add_privileged_reads", unsupported, strings.Replace(supported, "my.all", "db.all", 1), "新增的特权读取", 2},
		{"malformed_repair", unsupported, "malformed", string(ai.Invalid), 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			f.app.Model = ai.Func(func(ctx context.Context, r ai.Request) (ai.Response, error) {
				calls++
				if !strings.Contains(r.Messages[0].Content, "never use ILIKE") {
					t.Fatal("missing SQL subset contract")
				}
				if calls == 1 {
					return ai.Response{Text: encode(tc.first)}, nil
				}
				if calls > 2 || len(r.Messages) != 4 || r.Messages[2].Role != "assistant" || !strings.Contains(r.Messages[3].Content, "ILIKE") || !strings.Contains(r.Messages[3].Content, "preserving all visibility") {
					t.Fatal("invalid repair context", r.Messages)
				}
				if tc.second == "malformed" {
					return ai.Response{Text: "{}"}, nil
				}
				return ai.Response{Text: encode(tc.second)}, nil
			})
			out, err := f.app.Call(f.ctx, "assist_action", input)
			if tc.reason != "" {
				if err == nil || !strings.Contains(err.Error(), tc.reason) || out != nil {
					t.Fatalf("expected %s, got %v, %v", tc.reason, out, err)
				}
			} else {
				must(t, err)
				if out.(map[string]any)["script"] != supported {
					t.Fatal("corrected script missing", out)
				}
			}
			if calls != tc.calls {
				t.Fatalf("expected %d model calls, got %d", tc.calls, calls)
			}
		})
	}
	unchanged, err := f.c.Get(db.ID)
	must(t, err)
	if unchanged.Revision != db.Revision || unchanged.Actions[0].Script != def.Script {
		t.Fatal("repair changed saved draft")
	}
}

func TestAssistantMainContractAndReadBoundary(t *testing.T) {
	f, _ := setupAction(t, false)
	d, err := f.c.Get("default")
	must(t, err)
	script := `function main(params,context){const conn=db.session();return conn.queryOne("SELECT id FROM items WHERE owner_id=:userId",{userId:context.userId});}`
	d.Actions[0].Script = script
	must(t, f.c.Save(f.ctx, *d))
	d, err = f.c.Get(d.ID)
	must(t, err)
	def := d.Actions[0]
	in := application.Input{DatabaseID: d.ID, Revision: d.Revision, Name: def.Name, Assistant: &application.AssistantInput{Mode: "draft", Draft: &def, Intent: "Keep the existing ownership check"}}
	for _, tc := range []struct {
		script string
		reject bool
	}{
		{script, false},
		{`function main(params,context){return db.session().query("SELECT id, name FROM items");}`, true},
	} {
		f.app.Model = ai.Func(func(_ context.Context, r ai.Request) (ai.Response, error) {
			if !strings.Contains(r.Messages[0].Content, "Preserve main(params, context)") || strings.Contains(r.Messages[0].Content, "Use synchronous function run") {
				t.Fatal("wrong authoring contract")
			}
			raw, _ := json.Marshal(map[string]any{"parameters": def.Parameters, "script": tc.script})
			return ai.Response{Text: string(raw)}, nil
		})
		_, err = f.app.Call(f.ctx, "assist_action", in)
		if tc.reject && (err == nil || !strings.Contains(err.Error(), "特权读取")) {
			t.Fatalf("new native read accepted: %v", err)
		}
		if !tc.reject {
			must(t, err)
		}
	}
}

package managementmcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRejectInvalidManagementArguments(t *testing.T) {
	for _, tc := range []struct{ name, tool, raw string }{
		{"write through query", "datascope_query", `{"action":"save","args":{"resourceType":"policy","database_id":"default","database":{}}}`},
		{"ddl write through query", "datascope_query", `{"action":"apply","args":{"resourceType":"ddl","database_id":"default","plan_id":"p_x","request_id":"x"}}`},
		{"ddl foreign fields", "datascope_inspect", `{"action":"preview","args":{"resourceType":"ddl","database_id":"default","sql":"CREATE TABLE x(id)","write_password":"secret"}}`},
		{"wrong resource", "datascope_inspect", `{"action":"validate","args":{"resourceType":"user"}}`},
		{"missing resource", "datascope_query", `{"action":"list","args":{}}`},
		{"missing database", "datascope_query", `{"action":"get","args":{"resourceType":"database"}}`},
		{"foreign field", "datascope_query", `{"action":"list","args":{"resourceType":"database","password":"secret"}}`},
		{"null argument", "datascope_query", `{"action":"search","args":{"resourceType":"audit","limit":null}}`},
		{"string integer", "datascope_manage", `{"action":"enable","args":{"resourceType":"user","user_id":"42"}}`},
		{"zero user", "datascope_manage", `{"action":"enable","args":{"resourceType":"user","user_id":0}}`},
		{"negative pagination", "datascope_query", `{"action":"search","args":{"resourceType":"audit","offset":-1}}`},
		{"unknown nested field", "datascope_manage", `{"action":"create","args":{"resourceType":"database","database":{"id":"test","unexpected":true}}}`},
		{"mismatched database", "datascope_manage", `{"action":"save","args":{"resourceType":"policy","database_id":"one","database":{"id":"two","revision":1}}}`},
		{"missing revision", "datascope_manage", `{"action":"save","args":{"resourceType":"mcp","database_id":"one","database":{"id":"one"}}}`},
		{"missing replacement permissions", "datascope_manage", `{"action":"grant","args":{"resourceType":"grant","database_id":"default","user_id":42,"business_user_id":0}}`},
		{"missing identity binding", "datascope_manage", `{"action":"grant","args":{"resourceType":"grant","database_id":"default","user_id":42,"permissions":[]}}`},
		{"missing publish intent", "datascope_manage", `{"action":"save","args":{"resourceType":"knowledge","database_id":"default","id":"note","name":"Note","summary":"","content":"","revision":0}}`},
		{"retired knowledge tables", "datascope_manage", `{"action":"save","args":{"resourceType":"knowledge","database_id":"default","id":"note","name":"Note","description":"","content":"","tables":["items"],"published":true,"revision":0}}`},
		{"retired knowledge release flag", "datascope_manage", `{"action":"save","args":{"resourceType":"knowledge","database_id":"default","id":"note","name":"Note","description":"","content":"","release_knowledge_restrictions":true,"published":true,"revision":0}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var in input
			if err := json.Unmarshal([]byte(tc.raw), &in); err != nil {
				t.Fatal(err)
			}
			if _, _, err := decode(tc.tool, in); err == nil {
				t.Fatal("accepted invalid arguments")
			}
		})
	}
}

func TestDDLManagementOperationMatrix(t *testing.T) {
	for _, tc := range []struct {
		tool, action, domain string
		args                 map[string]any
	}{
		{"datascope_query", "list", "list_ddl_objects", map[string]any{"object_type": "table", "query": "clients", "limit": 20, "offset": 0}},
		{"datascope_query", "get", "get_ddl_object", map[string]any{"object_type": "table", "name": "clients"}},
		{"datascope_query", "execution", "get_ddl_execution", map[string]any{"execution_id": "e_1"}},
		{"datascope_query", "history", "list_ddl_executions", map[string]any{}},
		{"datascope_inspect", "preview", "preview_ddl", map[string]any{"changes": []map[string]any{{"operation": "drop", "object_type": "table", "name": "clients"}}}},
		{"datascope_manage", "apply", "apply_ddl", map[string]any{"plan_id": "p_1", "request_id": "apply-1"}},
	} {
		t.Run(tc.domain, func(t *testing.T) {
			tc.args["resourceType"] = "ddl"
			tc.args["database_id"] = "default"
			raw, _ := json.Marshal(map[string]any{"action": tc.action, "args": tc.args})
			var in input
			if err := json.Unmarshal(raw, &in); err != nil {
				t.Fatal(err)
			}
			domain, args, err := decode(tc.tool, in)
			if err != nil || domain != tc.domain || args.DatabaseID != "default" {
				t.Fatalf("%s %+v %v", domain, args, err)
			}
		})
	}
}

func TestExplicitEmptyReplacementValues(t *testing.T) {
	var in input
	if err := json.Unmarshal([]byte(`{"action":"save","args":{"resourceType":"knowledge","database_id":"default","id":"note","name":"Note","summary":"","content":"","published":false,"revision":0}}`), &in); err != nil {
		t.Fatal(err)
	}
	action, out, err := decode("datascope_manage", in)
	if err != nil || action != "save_knowledge" || out.Published || out.Revision != 0 {
		t.Fatalf("draft values lost: %s %+v %v", action, out, err)
	}
	in = input{}
	if err := json.Unmarshal([]byte(`{"action":"grant","args":{"resourceType":"grant","database_id":"default","user_id":42,"permissions":[],"business_user_id":0}}`), &in); err != nil {
		t.Fatal(err)
	}
	action, out, err = decode("datascope_manage", in)
	if err != nil || action != "grant_database_permissions" || out.Permissions == nil || out.BusinessUserID != 0 {
		t.Fatalf("replacement values lost: %s %+v %v", action, out, err)
	}
}

func TestActionDescriptionRoute(t *testing.T) {
	for _, name := range []string{"", "rename_item"} {
		op := "describe"
		domainName := "describe_action"
		argsIn := map[string]any{"resourceType": "action", "database_id": "default"}
		if name == "" {
			op = "list"
			domainName = "list_actions"
		} else {
			argsIn["name"] = name
		}
		var in input
		raw, _ := json.Marshal(map[string]any{"action": op, "args": argsIn})
		if err := json.Unmarshal(raw, &in); err != nil {
			t.Fatal(err)
		}
		domain, args, err := decode("datascope_query", in)
		if err != nil || domain != domainName || args.DatabaseID != "default" || args.Name != name {
			t.Fatalf("action description route: %s %+v %v", domain, args, err)
		}
		in.Args["script"] = json.RawMessage(`"injected"`)
		if _, _, err = decode("datascope_query", in); err == nil {
			t.Fatal("read-only description accepted script mutation")
		}
	}
}

func TestCreationAndConnectionTestAcceptPasswordWithoutAnID(t *testing.T) {
	for _, tool := range []string{"datascope_manage", "datascope_inspect"} {
		action, resource := "create", "database"
		if tool == "datascope_inspect" {
			action, resource = "test", "connection"
		}
		var in input
		raw := map[string]any{"action": action, "args": map[string]any{"resourceType": resource, "password": "database-password", "database": map[string]any{"name": "业务数据库", "driver": "mysql", "connection": map[string]any{"host": "db.local", "database": "business", "username": "reader"}}}}
		encoded, err := json.Marshal(raw)
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(encoded, &in); err != nil {
			t.Fatal(err)
		}
		_, out, err := decode(tool, in)
		if err != nil {
			t.Fatal(err)
		}
		if out.Database.ID != "" || out.Password != "database-password" {
			t.Fatal("generated identity or password input was lost")
		}
	}
}

func TestAuditPageAcceptsFiltersAndRejectsInvalidTypes(t *testing.T) {
	var in input
	if err := json.Unmarshal([]byte(`{"action":"page","args":{"resourceType":"audit","database_id":"default","actor_id":0,"status":"failed","source":"web","kind":"sql.execute","tool":"query","query":"timeout","started_from":"2026-10-28T00:00:00+08:00","started_to":"2026-10-28T23:59:59+08:00","limit":50,"offset":0}}`), &in); err != nil {
		t.Fatal(err)
	}
	action, out, err := decode("datascope_query", in)
	if err != nil || action != "get_audit_page" || out.ActorID == nil || *out.ActorID != 0 || out.Status != "failed" {
		t.Fatal("audit filters were not decoded", action, out, err)
	}
	for _, raw := range []string{`{"action":"page","args":{"resourceType":"audit","actor_id":-1}}`, `{"action":"page","args":{"resourceType":"audit","platform_only":"true"}}`, `{"action":"page","args":{"resourceType":"audit","status":null}}`} {
		if err := json.Unmarshal([]byte(raw), &in); err != nil {
			t.Fatal(err)
		}
		if _, _, err := decode("datascope_query", in); err == nil {
			t.Fatal("invalid audit filter accepted", raw)
		}
	}
}

func TestKnowledgeDescriptionArgumentsAndLegacyAlias(t *testing.T) {
	for _, text := range []string{`"description":"first\nsecond\nthird"`, `"summary":"first\nsecond\nthird"`, `"description":"","summary":"legacy"`} {
		var in input
		raw := `{"action":"save","args":{"resourceType":"knowledge","database_id":"default","id":"guide","name":"Guide",` + text + `,"content":"Body","published":true,"revision":0}}`
		if err := json.Unmarshal([]byte(raw), &in); err != nil {
			t.Fatal(err)
		}
		_, out, err := decode("datascope_manage", in)
		if err != nil || out.Description == nil {
			t.Fatalf("description rejected: %+v %v", out, err)
		}
		if text == `"description":"","summary":"legacy"` && *out.Description != "" {
			t.Fatal("explicit empty description overwritten")
		}
		if text != `"description":"","summary":"legacy"` && *out.Description != "first\nsecond\nthird" {
			t.Fatal(*out.Description)
		}
	}
}

func TestKnowledgeSearchOptionalKeywordAndRetiredPromptRoutes(t *testing.T) {
	for _, extra := range []string{"", `,"keyword":"active"`, `,"query":"legacy"`, `,"keyword":"active","cursor":"next"`} {
		var in input
		if err := json.Unmarshal([]byte(`{"action":"search","args":{"resourceType":"knowledge","database_id":"default"`+extra+`}}`), &in); err != nil {
			t.Fatal(err)
		}
		name, _, err := decode("datascope_query", in)
		if err != nil || name != "search_knowledge" {
			t.Fatal(name, err)
		}
	}
	for _, action := range []string{"list", "get"} {
		var in input
		if err := json.Unmarshal([]byte(`{"action":"`+action+`","args":{"resourceType":"knowledge_prompt","database_id":"default","name":"guide"}}`), &in); err != nil {
			t.Fatal(err)
		}
		if _, _, err := decode("datascope_query", in); err == nil {
			t.Fatal("retired prompt route accepted", action)
		}
	}
}

func TestConnectionManagementRoutes(t *testing.T) {
	for _, tc := range []struct{ tool, action, domain string }{
		{"datascope_manage", "edit_connection", "edit_database_connection"},
		{"datascope_inspect", "test_edited_connection", "test_edited_database_connection"},
	} {
		raw := `{"action":"` + tc.action + `","args":{"resourceType":"database","database_id":"target","database":{"id":"target","revision":2,"connection":{"host":"db.local","port":3306,"username":"reader"}},"password":"candidate-password"}}`
		var in input
		if err := json.Unmarshal([]byte(raw), &in); err != nil {
			t.Fatal(err)
		}
		domain, out, err := decode(tc.tool, in)
		if err != nil || domain != tc.domain || out.Password != "candidate-password" || out.Database.Connection.Host != "db.local" {
			t.Fatalf("%s %+v %v", domain, out, err)
		}
		for _, invalid := range []string{
			strings.Replace(raw, `"revision":2`, `"revision":0`, 1),
			strings.Replace(raw, `"id":"target"`, `"id":"other"`, 1),
			strings.Replace(raw, `,"connection":{"host":"db.local","port":3306,"username":"reader"}`, "", 1),
		} {
			if err := json.Unmarshal([]byte(invalid), &in); err != nil {
				t.Fatal(err)
			}
			if _, _, err := decode(tc.tool, in); err == nil {
				t.Fatal("accepted invalid connection edit")
			}
		}
		if err := json.Unmarshal([]byte(raw), &in); err != nil {
			t.Fatal(err)
		}
		if _, _, err := decode("datascope_query", in); err == nil {
			t.Fatal("query accepted connection edit")
		}
	}
	var in input
	if err := json.Unmarshal([]byte(`{"action":"remove","args":{"resourceType":"database","database_id":"target"}}`), &in); err != nil {
		t.Fatal(err)
	}
	if domain, _, err := decode("datascope_manage", in); err != nil || domain != "remove_database" {
		t.Fatal(domain, err)
	}
	if _, _, err := decode("datascope_inspect", in); err == nil {
		t.Fatal("inspect accepted removal")
	}
}

func TestDatabaseDocumentationPartialUpdate(t *testing.T) {
	for _, field := range []string{"description", "documentation"} {
		for _, value := range []string{`""`, `"业务说明"`} {
			raw := `{"action":"update","args":{"resourceType":"database","database_id":"default","database":{"id":"default","revision":1,"` + field + `":` + value + `}}}`
			var in input
			if err := json.Unmarshal([]byte(raw), &in); err != nil {
				t.Fatal(err)
			}
			domain, args, err := decode("datascope_manage", in)
			if err != nil || domain != "update_database" {
				t.Fatalf("partial update rejected: %s %v", raw, err)
			}
			if field == "description" && args.Database.Description == nil || field == "documentation" && args.Database.Documentation == nil {
				t.Fatal("explicit empty field lost")
			}
		}
	}
}

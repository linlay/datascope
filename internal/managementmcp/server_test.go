package managementmcp

import (
	"encoding/json"
	"testing"
)

func TestRejectInvalidManagementArguments(t *testing.T) {
	for _, tc := range []struct{ name, tool, raw string }{
		{"write through query", "datascope_query", `{"action":"save","args":{"resourceType":"policy","database_id":"default","database":{}}}`},
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
		{"missing publish intent", "datascope_manage", `{"action":"save","args":{"resourceType":"knowledge","database_id":"default","id":"note","name":"Note","summary":"","content":"","tables":[],"revision":0}}`},
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

func TestExplicitEmptyReplacementValues(t *testing.T) {
	var in input
	if err := json.Unmarshal([]byte(`{"action":"save","args":{"resourceType":"knowledge","database_id":"default","id":"note","name":"Note","summary":"","content":"","tables":[],"published":false,"revision":0}}`), &in); err != nil {
		t.Fatal(err)
	}
	action, out, err := decode("datascope_manage", in)
	if err != nil || action != "save_knowledge" || out.Published || out.Revision != 0 || out.Tables == nil {
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

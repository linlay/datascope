package platform_test

import (
	"encoding/json"
	"strings"
	"testing"

	"datascope-server/internal/application"
	"datascope-server/internal/management"
)

func TestActionWorkbenchMetadataAndPermissions(t *testing.T) {
	f, business := setupAction(t, true)
	d, err := f.c.Get("default")
	must(t, err)
	d.MCPEnabled = true
	must(t, f.c.Save(f.ctx, *d))
	for _, name := range []string{"", "rename_item"} {
		operation := "list_actions"
		if name != "" {
			operation = "describe_action"
		}
		live, e := f.app.DescribeAction(business, "default", name)
		must(t, e)
		preview, e := f.app.Call(f.ctx, operation, application.Input{DatabaseID: "default", Name: name})
		must(t, e)
		want, _ := json.Marshal(live)
		got, _ := json.Marshal(preview)
		if string(got) != string(want) || strings.Contains(string(got), `"script"`) {
			t.Fatalf("workbench must share public MCP metadata: %s != %s", got, want)
		}
		if (name != "") != strings.Contains(string(got), `"parameters"`) {
			t.Fatalf("parameter schema must be disclosed on demand: %s", got)
		}
	}
	if _, err = f.app.Call(f.readerContext(), "list_actions", application.Input{DatabaseID: "default"}); err == nil {
		t.Fatal("query permission exposed action metadata")
	}
	// Viewing a schema needs action.write, but no connection details or simulation grant.
	must(t, f.m.SaveGrant(f.ctx, f.admin.ID, management.Grant{UserID: f.reader.ID, DatabaseID: "default", BusinessUserID: 10, Permissions: []string{"action.write"}}))
	_, err = f.app.Call(f.readerContext(), "describe_action", application.Input{DatabaseID: "default", Name: "rename_item"})
	must(t, err)
	in := application.Input{DatabaseID: "default", Name: "rename_item", BusinessUserID: 10, RequestID: "no-simulation-grant", Arguments: map[string]any{"id": 1, "name": "changed"}}
	if _, err = f.app.Call(f.readerContext(), "try_action", in); err == nil {
		t.Fatal("schema access allowed identity simulation")
	}
	if _, err = f.app.Call(f.readerContext(), "list_actions", application.Input{DatabaseID: "another"}); err == nil {
		t.Fatal("metadata crossed database grant boundary")
	}
	if _, err = f.app.Call(business, "list_actions", application.Input{DatabaseID: "default"}); err == nil {
		t.Fatal("business identity used platform inspection")
	}
	if _, err = f.app.Call(f.ctx, "describe_action", application.Input{DatabaseID: "default", Name: "missing"}); err == nil {
		t.Fatal("unknown action returned metadata")
	}
	// Metadata remains available while configuring an offline or write-disabled database.
	d, err = f.c.Get("default")
	must(t, err)
	d.MCPEnabled = false
	d.WriteEnabled = false
	must(t, f.c.Save(f.ctx, *d))
	_, err = f.app.Call(f.ctx, "list_actions", application.Input{DatabaseID: "default"})
	must(t, err)
	if _, err = f.app.DescribeAction(business, "default", ""); err == nil {
		t.Fatal("workbench weakened live MCP gates")
	}
	if _, err = f.app.Call(f.ctx, "try_action", in); err == nil {
		t.Fatal("disabled writer executed a trial")
	}
}

func TestActionWorkbenchTrialRollbackAndReplay(t *testing.T) {
	for _, confirm := range []bool{false, true} {
		f, business := setupAction(t, confirm)
		in := application.Input{DatabaseID: "default", Name: "rename_item", BusinessUserID: 10, RequestID: "workbench-trial", Arguments: map[string]any{"id": 1, "name": "preview"}}
		first, err := f.app.Call(f.ctx, "try_action", in)
		must(t, err)
		out := first.(*application.ActionResponse)
		if out.Status != "preview" || out.Preview == nil || len(out.Preview.Writes) != 1 || out.ConfirmationToken != "" {
			t.Fatalf("expected rollback preview, got %+v", out)
		}
		rows, err := f.st.Query(business, "SELECT name FROM items WHERE id=1", 1)
		must(t, err)
		if rows.Rows[0]["name"] != "one" {
			t.Fatal("workbench committed business changes")
		}
		again, err := f.app.Call(f.ctx, "try_action", in)
		must(t, err)
		got, _ := json.Marshal(again)
		want, _ := json.Marshal(first)
		if string(got) != string(want) {
			t.Fatal("same request must return the saved preview")
		}
		in.Arguments = map[string]any{"id": 2, "name": "forbidden"}
		if _, err = f.app.Call(f.ctx, "try_action", in); err == nil {
			t.Fatal("request_id accepted different arguments")
		}
		in.RequestID = "workbench-denied"
		denied, err := f.app.Call(f.ctx, "try_action", in)
		must(t, err)
		if denied.(*application.ActionResponse).Status != "denied" {
			t.Fatal("trial bypassed row permissions")
		}
	}
}

package platform_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"

	"datascope-server/internal/action"
	"datascope-server/internal/application"
	"datascope-server/internal/audit"
	"datascope-server/internal/guard"
	"datascope-server/internal/management"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func setupAction(t *testing.T, confirm bool) (*fixture, context.Context) {
	t.Helper()
	f := setup(t)
	d, e := f.c.Get("default")
	must(t, e)
	d.WriteEnabled = true
	d.Tools = append(d.Tools, "list_actions", "describe_action", "invoke_action")
	d.Actions = []action.Definition{{Name: "rename_item", Title: "Rename", Description: "Rename owned item", Confirm: confirm, Parameters: map[string]action.Parameter{"id": {Type: "integer"}, "name": {Type: "string"}}, Script: "function run(args,ctx){if(!my.one`SELECT id FROM items WHERE id=${args.id}`)deny('invisible');const n=db.exec`UPDATE items SET name=${args.name} WHERE id=${args.id} AND owner_id=${ctx.userId}`;if(n!==1)deny('changed');return {id:args.id,name:args.name}}"}}
	must(t, f.c.Save(f.ctx, *d))
	a, e := f.m.SaveBusinessAccount(f.ctx, management.BusinessAccount{DatabaseID: "default", Username: "E1", Name: "Employee", BusinessUserID: 10, Active: true, Permissions: []string{"action"}}, "business-password", 0)
	must(t, e)
	ctx := guard.WithBusiness(audit.With(context.Background(), audit.Metadata{Source: "test"}), a)
	return f, ctx
}
func TestActionDurableIdempotencyAndPermission(t *testing.T) {
	f, ctx := setupAction(t, false)
	args := map[string]any{"id": 1, "name": "after"}
	out, e := f.app.InvokeAction(ctx, "default", "rename_item", "request-1", args)
	must(t, e)
	if out.Status != "done" {
		t.Fatal(out)
	}
	d, e := f.c.Get("default")
	must(t, e)
	writer, e := f.c.OpenWriter(ctx, d)
	must(t, e)
	tx, e := writer.BeginAction(ctx)
	must(t, e)
	_, e = tx.Exec(ctx, "UPDATE items SET name='external' WHERE id=1")
	must(t, e)
	must(t, tx.Commit())
	writer.Close()
	// Reopen management to represent a server restart; the receipt still wins.
	other, e := management.Open(f.m.Path, f.a)
	must(t, e)
	defer other.Close()
	app := *f.app
	app.Management = other
	again, e := app.InvokeAction(ctx, "default", "rename_item", "request-1", args)
	must(t, e)
	if again.Status != "done" {
		t.Fatal(again)
	}
	rows, e := f.st.Query(ctx, "SELECT name FROM items WHERE id=1", 1)
	must(t, e)
	if rows.Rows[0]["name"] != "external" {
		t.Fatal("duplicate write")
	}
	if _, e = f.app.InvokeAction(ctx, "default", "rename_item", "request-1", map[string]any{"id": 1, "name": "different"}); e == nil {
		t.Fatal("reused id accepted")
	}
	if _, e = f.app.InvokeAction(f.readerContext(), "default", "rename_item", "unauthorized", args); e == nil {
		t.Fatal("platform substituted business account")
	}
	out, e = f.app.InvokeAction(ctx, "default", "rename_item", "invisible", map[string]any{"id": 2, "name": "attack"})
	must(t, e)
	if out.Status != "denied" {
		t.Fatal(out)
	}
	if _, e = f.app.InvokeAction(ctx, "default", "rename_item", "schema", map[string]any{"id": 1}); e == nil || !strings.Contains(e.Error(), "additionalProperties") {
		t.Fatal(e)
	}
}
func TestActionConfirmationAndConcurrentReplay(t *testing.T) {
	f, ctx := setupAction(t, true)
	args := map[string]any{"id": 1, "name": "confirmed"}
	out, e := f.app.InvokeAction(ctx, "default", "rename_item", "confirm", args)
	must(t, e)
	if out.Status != "confirm_required" || out.ConfirmationToken == "" {
		t.Fatal(out)
	}
	rows, e := f.st.Query(ctx, "SELECT name FROM items WHERE id=1", 1)
	must(t, e)
	if rows.Rows[0]["name"] != "one" {
		t.Fatal("preview wrote data")
	}
	if _, e = f.app.ConfirmAction(ctx, out.ConfirmationToken, "wrong"); e == nil {
		t.Fatal("password bypass")
	}
	var wg sync.WaitGroup
	success := make(chan bool, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := f.app.ConfirmAction(context.Background(), out.ConfirmationToken, "business-password")
			success <- e == nil && r.Status == "done"
		}()
	}
	wg.Wait()
	close(success)
	n := 0
	for ok := range success {
		if ok {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("expected one confirmation, got %d", n)
	}
	again, e := f.app.InvokeAction(ctx, "default", "rename_item", "confirm", args)
	must(t, e)
	if again.Status != "done" {
		t.Fatal(again)
	}
	if _, e = f.app.ConfirmAction(ctx, out.ConfirmationToken, "business-password"); e == nil {
		t.Fatal("token replay")
	}
}
func TestActionExpiredChangedAndRevokedConfirmation(t *testing.T) {
	for _, mode := range []string{"expired", "config", "account", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			f, ctx := setupAction(t, true)
			out, e := f.app.InvokeAction(ctx, "default", "rename_item", "pending", map[string]any{"id": 1, "name": "blocked"})
			must(t, e)
			switch mode {
			case "expired":
				_, e = sourceDB(t, f, "default").Exec("UPDATE ACTION_RECEIPT SET EXPIRES_=0")
			case "config":
				d, err := f.c.Get("default")
				must(t, err)
				d.Name = "Changed"
				e = f.c.Save(f.ctx, *d)
			case "account":
				a, _ := guard.Business(ctx)
				a.Permissions = nil
				_, e = f.m.SaveBusinessAccount(f.ctx, a, "", a.AuthVersion)
			case "disabled":
				d, err := f.c.Get("default")
				must(t, err)
				d.MCPEnabled = false
				e = f.c.Save(f.ctx, *d)
			}
			must(t, e)
			if _, e = f.app.ConfirmAction(ctx, out.ConfirmationToken, "business-password"); e == nil {
				t.Fatal("stale confirmation accepted")
			}
			rows, e := f.st.Query(ctx, "SELECT name FROM items WHERE id=1", 1)
			must(t, e)
			if rows.Rows[0]["name"] != "one" {
				t.Fatal("stale confirmation wrote data")
			}
		})
	}
}
func TestActionHTTPConfirmationCannotBeInvokedByMCP(t *testing.T) {
	f, ctx := setupAction(t, true)
	server := api(t, f)
	f.app.ManagementMCPURL = server.URL + "/mcp/admin"
	out, e := f.app.InvokeAction(ctx, "default", "rename_item", "http", map[string]any{"id": 1, "name": "http-confirmed"})
	must(t, e)
	// The confirmation route accepts a browser nonce cookie and password, not a
	// JSON tool argument or possession of the model-visible token alone.
	status, _ := request(t, http.DefaultClient, "POST", server.URL+"/actions/confirm", map[string]any{"token": out.ConfirmationToken})
	if status != 403 {
		t.Fatal(status)
	}
	jar, e := cookiejar.New(nil)
	must(t, e)
	client := &http.Client{Jar: jar}
	status, _, body := formRequest(t, client, "GET", out.ConfirmationURL, nil)
	if status != 200 || !strings.Contains(body, "确认并执行") {
		t.Fatal(status, body)
	}
	nonce := regexp.MustCompile(`name="nonce" value="([a-f0-9]+)"`).FindStringSubmatch(body)
	if len(nonce) != 2 {
		t.Fatal(body)
	}
	status, _, body = formRequest(t, client, "POST", out.ConfirmationURL, url.Values{"token": {out.ConfirmationToken}, "nonce": {nonce[1]}, "password": {"business-password"}})
	if status != 200 || !strings.Contains(body, "done") {
		t.Fatal(status, body)
	}
}
func TestActionWritePermissionAndDryRun(t *testing.T) {
	f, ctx := setupAction(t, false)
	d, e := f.c.Get("default")
	must(t, e)
	// mcp.write alone cannot replace JavaScript or enable a write account.
	must(t, f.m.SaveGrant(f.ctx, f.admin.ID, management.Grant{UserID: f.reader.ID, DatabaseID: "default", BusinessUserID: 10, Permissions: []string{"config.read", "mcp.write"}}))
	candidate := *d
	candidate.Actions = nil
	if _, e = f.app.Call(f.readerContext(), "save_database", application.Input{DatabaseID: "default", Database: &candidate}); e == nil {
		t.Fatal("mcp.write edited actions")
	}
	if _, e = f.app.Call(f.readerContext(), "configure_actions", application.Input{DatabaseID: "default", Database: &candidate}); e == nil {
		t.Fatal("action.write bypass")
	}
	out, e := f.app.Call(f.ctx, "try_action", application.Input{DatabaseID: "default", Name: "rename_item", BusinessUserID: 10, RequestID: "trial", Arguments: map[string]any{"id": 1, "name": "trial"}})
	must(t, e)
	result := out.(*application.ActionResponse)
	if result.Status != "preview" || len(result.SQL) == 0 {
		t.Fatal(result)
	}
	rows, e := f.st.Query(ctx, "SELECT name FROM items WHERE id=1", 1)
	must(t, e)
	if rows.Rows[0]["name"] != "one" {
		t.Fatal("trial committed")
	}
}
func TestActionMCPToolsAndLiveRevocation(t *testing.T) {
	env := setupRealms(t)
	f := env.f
	d, e := f.c.Get("default")
	must(t, e)
	d.WriteEnabled = true
	d.Tools = append(d.Tools, "list_actions", "describe_action", "invoke_action")
	d.Actions = []action.Definition{{Name: "noop", Title: "Noop", Description: "Noop", Parameters: map[string]action.Parameter{}, Script: "function run(){return {ok:true}}"}}
	must(t, f.c.Save(f.ctx, *d))
	a := env.accounts["default"]
	a.Permissions = []string{"action"}
	a, e = f.m.SaveBusinessAccount(f.ctx, a, "", a.AuthVersion)
	must(t, e)
	tokens, _, _ := employeeTokens(t, env, "default", "employee-default-password")
	session, e := mcp.NewClient(&mcp.Implementation{Name: "action-test", Version: "1"}, nil).Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: env.resource + "/default", HTTPClient: &http.Client{Transport: bearer{token: tokens["access"]}}, DisableStandaloneSSE: true}, nil)
	must(t, e)
	defer session.Close()
	listed, e := session.ListTools(context.Background(), nil)
	must(t, e)
	names := []string{}
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}
	if len(names) != 3 {
		t.Fatal(names)
	}
	list, e := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_actions", Arguments: map[string]any{}})
	must(t, e)
	listRaw, _ := json.Marshal(list)
	if list.IsError || !strings.Contains(string(listRaw), "noop") || strings.Contains(string(listRaw), "additionalProperties") {
		t.Fatal(string(listRaw))
	}
	if out, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "describe_action", Arguments: map[string]any{}}); err == nil && !out.IsError {
		t.Fatal("describe_action must require a name; use list_actions for the list")
	}
	res, e := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "describe_action", Arguments: map[string]any{"name": "noop"}})
	must(t, e)
	raw, _ := json.Marshal(res)
	if res.IsError || !strings.Contains(string(raw), "additionalProperties") {
		t.Fatal(string(raw))
	}
	res, e = session.CallTool(context.Background(), &mcp.CallToolParams{Name: "invoke_action", Arguments: map[string]any{"name": "noop", "arguments": map[string]any{}, "request_id": "mcp"}})
	must(t, e)
	raw, _ = json.Marshal(res)
	if res.IsError || !strings.Contains(string(raw), "done") {
		t.Fatal(string(raw))
	}
	a.Permissions = nil
	_, e = f.m.SaveBusinessAccount(f.ctx, a, "", a.AuthVersion)
	must(t, e)
	if res, e = session.CallTool(context.Background(), &mcp.CallToolParams{Name: "describe_action", Arguments: map[string]any{}}); e == nil && !res.IsError {
		t.Fatal("revoked action still callable")
	}
	if res, e = session.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_actions", Arguments: map[string]any{}}); e == nil && !res.IsError {
		t.Fatal("revoked action list still callable")
	}
}

func TestActionConcurrentInvokeAndUnknownReceipt(t *testing.T) {
	f, ctx := setupAction(t, false)
	args := map[string]any{"id": 1, "name": "once"}
	var wg sync.WaitGroup
	fail := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, e := f.app.InvokeAction(ctx, "default", "rename_item", "concurrent", args)
			if e != nil && !strings.Contains(e.Error(), "结果未知") {
				fail <- e
			}
			if e == nil && out.Status != "done" {
				fail <- fmt.Errorf("unexpected: %v", out)
			}
		}()
	}
	wg.Wait()
	close(fail)
	for e := range fail {
		t.Fatal(e)
	}
	events, e := f.a.List(ctx, "default", "concurrent", 100, 0)
	must(t, e)
	writes := 0
	for _, ev := range events {
		if ev.Kind == "action.sql" && strings.Contains(ev.ExecutedSQL, "UPDATE") {
			writes++
		}
	}
	if writes != 1 {
		t.Fatal("repeated business execution", writes)
	}
	// Simulate a process crash after reserving a new request. Never reclaim it.
	_, e = sourceDB(t, f, "default").Exec("UPDATE ACTION_RECEIPT SET STATE_='running',DATA_=json_remove(DATA_,'$.response') WHERE REQUEST_ID_='concurrent'")
	must(t, e)
	if _, e = f.app.InvokeAction(ctx, "default", "rename_item", "concurrent", args); e == nil || !strings.Contains(e.Error(), "结果未知") {
		t.Fatal(e)
	}
}

func TestActionCommitsWithUnavailableAudit(t *testing.T) {
	f, ctx := setupAction(t, false)
	must(t, f.a.Close())
	must(t, f.m.AuditFor("default").Close())
	out, e := f.app.InvokeAction(ctx, "default", "rename_item", "audit-offline", map[string]any{"id": 1, "name": "committed-without-audit"})
	must(t, e)
	if out.Status != "done" {
		t.Fatal(out)
	}
	rows, e := f.st.Query(ctx, "SELECT name FROM items WHERE id=1", 1)
	must(t, e)
	if rows.Rows[0]["name"] != "committed-without-audit" {
		t.Fatal(rows)
	}
	var state string
	must(t, sourceDB(t, f, "default").QueryRow("SELECT STATE_ FROM ACTION_RECEIPT WHERE REQUEST_ID_='audit-offline'").Scan(&state))
	if state != "done" {
		t.Fatal(state)
	}
}

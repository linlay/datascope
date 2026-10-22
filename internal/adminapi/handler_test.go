package adminapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sql-rewrite-server/internal/testutil"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"sql-rewrite-server/internal/mcpserver"
	"sql-rewrite-server/internal/rewrite"
	"sql-rewrite-server/internal/service"
	"sql-rewrite-server/internal/store"
)

func newAPI(t *testing.T, opts mcpserver.Options) *httptest.Server {
	t.Helper()
	st, err := store.Open(testutil.Demo(t), 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cfg, err := rewrite.LoadConfig(filepath.Join("..", "..", "config", "rewrite.yml"))
	if err != nil {
		t.Fatal(err)
	}
	svc, err := service.New(context.Background(), st, cfg, store.DefaultAsOf)
	if err != nil {
		t.Fatal(err)
	}
	server := mcpserver.New(svc, opts)
	mcpHTTP := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true}))
	t.Cleanup(mcpHTTP.Close)

	api := httptest.NewServer(Handler(svc, &Inspector{
		Endpoint:    mcpHTTP.URL,
		Service:     svc,
		FixturesDir: filepath.Join("..", "..", "fixtures"),
	}))
	t.Cleanup(api.Close)
	return api
}

func do(t *testing.T, method, url string, body any, out any) int {
	t.Helper()
	raw, _ := json.Marshal(body)
	if body == nil {
		raw = nil
	}
	req, _ := http.NewRequest(method, url, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatal(err)
		}
	}
	return resp.StatusCode
}

func runVerify(t *testing.T, opts mcpserver.Options) Report {
	t.Helper()
	var report Report
	if code := do(t, "POST", newAPI(t, opts).URL+"/api/mcp/verify", nil, &report); code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if report.ProtocolVersion != "2026-07-28" {
		t.Errorf("protocol = %s", report.ProtocolVersion)
	}
	if report.Failed != 0 {
		for _, c := range report.Checks {
			if c.Status == StatusFail {
				t.Errorf("%s / %s: %s\n expected %v\n actual   %v", c.Group, c.Name, c.Detail, c.Expected, c.Actual)
			}
		}
	}
	return report
}

func TestVerifyAgainstLiveMCPServer(t *testing.T) {
	report := runVerify(t, mcpserver.Options{})
	// Production mode skips the five raw_query checks.
	if report.Skipped != 5 || report.Warned != 0 || report.Passed < 25 {
		t.Errorf("passed=%d warned=%d skipped=%d", report.Passed, report.Warned, report.Skipped)
	}

	debug := runVerify(t, mcpserver.Options{Debug: true})
	// Debug mode is reported as a warning; administrator raw_query checks now run.
	if debug.Skipped != 0 || debug.Warned != 1 || debug.Passed <= report.Passed {
		t.Errorf("debug passed=%d warned=%d skipped=%d", debug.Passed, debug.Warned, debug.Skipped)
	}

	withDefault := runVerify(t, mcpserver.Options{DefaultUserID: 1})
	if withDefault.Warned != 1 {
		t.Errorf("default user not flagged: warned=%d", withDefault.Warned)
	}
}

func TestMCPInfoAndCallAsUser(t *testing.T) {
	api := newAPI(t, mcpserver.Options{})
	var info ServerInfo
	if code := do(t, "GET", api.URL+"/api/mcp/info", nil, &info); code != http.StatusOK || len(info.Tools) != 3 || info.Server.Name != mcpserver.Name {
		t.Fatalf("info: status %d, %+v", code, info)
	}
	var call struct {
		Result struct {
			IsError           bool           `json:"isError"`
			StructuredContent map[string]any `json:"structuredContent"`
		} `json:"result"`
	}
	do(t, "POST", api.URL+"/api/mcp/call", map[string]any{"name": "query", "user_id": 121, "arguments": map[string]any{"sql": "SELECT customer_no FROM customers"}}, &call)
	if call.Result.IsError || call.Result.StructuredContent["row_count"].(float64) != 54 {
		t.Fatalf("call result = %+v", call.Result)
	}
}

func TestRewritePreview(t *testing.T) {
	api := newAPI(t, mcpserver.Options{})
	var res struct {
		RewrittenSQL string           `json:"rewritten_sql"`
		RowCount     int              `json:"row_count"`
		Tables       []map[string]any `json:"tables"`
	}
	if code := do(t, "POST", api.URL+"/api/rewrite", map[string]any{"sql": "SELECT * FROM fund_accounts", "user_id": 121}, &res); code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if res.RowCount != 54 || res.RewrittenSQL == "" || res.Tables[0]["scheme"] != "CUSTOMER" {
		t.Fatalf("rewrite preview = %+v", res)
	}
	var denied struct {
		Error  string `json:"error"`
		Denied bool   `json:"denied"`
	}
	if code := do(t, "POST", api.URL+"/api/rewrite", map[string]any{"sql": "SELECT * FROM employee_scopes", "user_id": 1}, &denied); code != http.StatusBadRequest || !denied.Denied {
		t.Fatalf("restricted preview: status %d %+v", code, denied)
	}
}

func TestRewriteOnlyDoesNotExecute(t *testing.T) {
	api := newAPI(t, mcpserver.Options{})
	// A missing column can be rewritten, but executing it must fail.
	input := map[string]any{"sql": "SELECT c.missing_preview_column FROM customers c", "user_id": 1, "preview_only": true}
	var preview map[string]any
	if code := do(t, "POST", api.URL+"/api/rewrite", input, &preview); code != http.StatusOK {
		t.Fatalf("preview status %d: %+v", code, preview)
	}
	if preview["rewritten_sql"] == "" || preview["binds"] == nil {
		t.Fatalf("missing rewrite metadata: %+v", preview)
	}
	if _, exists := preview["rows"]; exists {
		t.Fatalf("preview unexpectedly includes execution results: %+v", preview)
	}
	input["preview_only"] = false
	var execution map[string]any
	if code := do(t, "POST", api.URL+"/api/rewrite", input, &execution); code != http.StatusBadRequest || execution["rewrite"] == nil {
		t.Fatalf("expected execution failure with rewrite metadata, status %d: %+v", code, execution)
	}
	var denied map[string]any
	if code := do(t, "POST", api.URL+"/api/rewrite", map[string]any{"sql": "SELECT * FROM employee_scopes", "user_id": 1, "preview_only": true}, &denied); code != http.StatusBadRequest || denied["denied"] != true {
		t.Fatalf("preview bypassed permission guard, status %d: %+v", code, denied)
	}
}

func TestPoliciesAndUsers(t *testing.T) {
	api := newAPI(t, mcpserver.Options{})
	var policies struct {
		Schemes  []map[string]any `json:"schemes"`
		Policies []map[string]any `json:"policies"`
	}
	do(t, "GET", api.URL+"/api/policies", nil, &policies)
	if len(policies.Schemes) != 4 || len(policies.Policies) != 17 {
		t.Fatalf("schemes=%d policies=%d", len(policies.Schemes), len(policies.Policies))
	}
	var users []store.UserSummary
	do(t, "GET", api.URL+"/api/users", nil, &users)
	if len(users) != 100 || users[0].Roles != "客户经理" {
		t.Fatalf("users = %d, first %+v", len(users), users[0])
	}
}

func TestBrowseRows(t *testing.T) {
	api := newAPI(t, mcpserver.Options{})
	var page store.BrowseResult
	do(t, "GET", api.URL+"/api/tables/customers/rows?limit=10&offset=5&order=customer_no", nil, &page)
	if page.Total != 1500 || len(page.Rows) != 10 || page.Rows[0]["customer_no"].(string) != "C00000006" {
		t.Fatalf("page = total %d rows %d first %v", page.Total, len(page.Rows), page.Rows[0]["customer_no"])
	}
	do(t, "GET", api.URL+"/api/tables/fund_accounts/rows?col=department_code&val=D002&q=C0000", nil, &page)
	if page.Total != 69 { // Fund accounts opened in department 2
		t.Fatalf("filtered total = %d", page.Total)
	}
	var errBody map[string]string
	if code := do(t, "GET", api.URL+"/api/tables/customers/rows?order=nope", nil, &errBody); code != http.StatusBadRequest {
		t.Fatalf("bad order column status = %d", code)
	}
	if code := do(t, "GET", api.URL+"/api/tables/nope/rows", nil, &errBody); code != http.StatusNotFound {
		t.Fatalf("missing table status = %d", code)
	}
}

package mcpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sql-rewrite-server/internal/catalog"
	"sql-rewrite-server/internal/testutil"
	"strconv"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"sql-rewrite-server/internal/rewrite"
	"sql-rewrite-server/internal/service"
	"sql-rewrite-server/internal/store"
)

// userTransport plays the gateway: it stamps the trusted user header.
type userTransport struct{ userID int64 }

func (t userTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	if t.userID != 0 {
		r.Header.Set(DefaultUserHeader, strconv.FormatInt(t.userID, 10))
	}
	return http.DefaultTransport.RoundTrip(r)
}

func startServer(t *testing.T, opts Options) string {
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
	server := New(svc, opts)
	httpServer := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true}))
	t.Cleanup(httpServer.Close)
	return httpServer.URL
}

func connect(t *testing.T, url, protocolVersion string, userID int64) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:   url,
		HTTPClient: &http.Client{Transport: userTransport{userID}},
	}, &mcp.ClientSessionOptions{ProtocolVersion: protocolVersion})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

func call(t *testing.T, session *mcp.ClientSession, name string, args map[string]any, out any) *mcp.CallToolResult {
	t.Helper()
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if out != nil && !res.IsError {
		raw, _ := json.Marshal(res.StructuredContent)
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatal(err)
		}
	}
	return res
}

func toolNames(t *testing.T, session *mcp.ClientSession) []string {
	t.Helper()
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("tool %s is not marked read-only", tool.Name)
		}
	}
	slices.Sort(names)
	return names
}

type queryResult struct {
	RowCount     int              `json:"row_count"`
	Rows         []map[string]any `json:"rows"`
	RewrittenSQL string           `json:"rewritten_sql"`
}

func customerIDs(r queryResult) []string {
	var ids []string
	for _, row := range r.Rows {
		ids = append(ids, row["customer_no"].(string))
	}
	return ids
}

func TestRewrittenQueryOverHTTP(t *testing.T) {
	url := startServer(t, Options{})
	for _, version := range []string{"2026-07-28", "2025-11-25", "2025-06-18"} {
		t.Run(version, func(t *testing.T) {
			session := connect(t, url, version, 1)
			if got := session.InitializeResult().ProtocolVersion; got != version {
				t.Fatalf("negotiated %q, want %q", got, version)
			}
			if names := toolNames(t, session); !slices.Equal(names, []string{"describe_table", "list_tables", "query"}) {
				t.Fatalf("tools = %v", names)
			}

			var tables struct {
				Tables []struct {
					Name   string                  `json:"name"`
					Access struct{ Scheme string } `json:"access"`
				} `json:"tables"`
			}
			call(t, session, "list_tables", map[string]any{}, &tables)
			if len(tables.Tables) != 17 {
				t.Errorf("list_tables returned %d tables", len(tables.Tables))
			}
			for _, tbl := range tables.Tables {
				if tbl.Access.Scheme == "" {
					t.Errorf("table %s has no access scheme", tbl.Name)
				}
			}

			var result queryResult
			call(t, session, "query", map[string]any{"sql": "SELECT customer_no FROM customers ORDER BY customer_no"}, &result)
			var fixtures []struct {
				UserID int64    `json:"user_id"`
				IDs    []string `json:"expected_customer_ids"`
			}
			raw, _ := os.ReadFile("../../fixtures/permission_cases.json")
			json.Unmarshal(raw, &fixtures)
			if !slices.Equal(customerIDs(result), fixtures[0].IDs) {
				t.Fatalf("manager scope mismatch")
			}

			if result.RewrittenSQL != "" {
				t.Error("rewritten SQL leaked outside debug mode")
			}
			for _, sql := range []string{"DELETE FROM customers", "SELECT * FROM employee_scopes"} {
				if res := call(t, session, "query", map[string]any{"sql": sql}, nil); !res.IsError {
					t.Errorf("%q was not rejected", sql)
				}
			}
		})
	}
}

func TestQueryRequiresIdentity(t *testing.T) {
	session := connect(t, startServer(t, Options{}), "", 0)
	if res := call(t, session, "query", map[string]any{"sql": "SELECT customer_no FROM customers"}, nil); !res.IsError {
		t.Fatal("query without identity was not rejected")
	}

	// A configured default applies only when the header is absent.
	url := startServer(t, Options{DefaultUserID: 121})
	var result queryResult
	call(t, connect(t, url, "", 0), "query", map[string]any{"sql": "SELECT customer_no FROM customers ORDER BY customer_no"}, &result)
	if result.RowCount != 54 {
		t.Fatalf("department sees %d", result.RowCount)
	}

	call(t, connect(t, url, "", 175), "query", map[string]any{"sql": "SELECT customer_no FROM customers", "max_rows": 1000}, &result)
	if result.RowCount != 1000 {
		t.Errorf("header employee 175 saw %d customers", result.RowCount)
	}
}

func TestDebugTools(t *testing.T) {
	session := connect(t, startServer(t, Options{Debug: true}), "", 175)
	if names := toolNames(t, session); !slices.Equal(names, []string{"customer_scope", "describe_table", "list_tables", "query", "raw_query"}) {
		t.Fatalf("tools = %v", names)
	}
	var raw queryResult
	call(t, session, "raw_query", map[string]any{"sql": "SELECT customer_no FROM customers", "max_rows": 1000}, &raw)
	if raw.RowCount != 1000 {
		t.Errorf("raw_query saw %d customers", raw.RowCount)
	}
	call(t, session, "raw_query", map[string]any{"sql": "PRAGMA table_info(roles)"}, &raw)
	if raw.RowCount != 4 { // roles has 4 columns
		t.Errorf("raw PRAGMA returned %d rows", raw.RowCount)
	}
	if res := call(t, session, "raw_query", map[string]any{"sql": "DELETE FROM customers"}, nil); !res.IsError {
		t.Error("raw_query wrote to a read-only database")
	}

	var rewritten queryResult
	call(t, session, "query", map[string]any{"sql": "SELECT customer_no FROM customers"}, &rewritten)
	if rewritten.RowCount != 200 || rewritten.RewrittenSQL == "" {
		t.Errorf("debug query = %+v", rewritten)
	}

	var scope struct {
		CustomerIDs []string `json:"customer_ids"`
	}
	call(t, session, "customer_scope", map[string]any{"user_id": 1}, &scope)
	if len(scope.CustomerIDs) != 27 {
		t.Errorf("customer_scope(1) = %v", scope.CustomerIDs)
	}
}

func TestDebugRequiresAdministrator(t *testing.T) {
	url := startServer(t, Options{Debug: true})
	s := connect(t, url, "", 1)
	for _, tool := range []string{"raw_query", "customer_scope"} {
		args := map[string]any{"sql": "SELECT * FROM customers", "user_id": 175}
		if !call(t, s, tool, args, nil).IsError {
			t.Fatalf("ordinary employee could use %s", tool)
		}
	}
}
func TestNoneIgnoresIdentityHeader(t *testing.T) {
	url := startServer(t, Options{AuthMode: "none", DefaultUserID: 1})
	s := connect(t, url, "", 175)
	var r queryResult
	call(t, s, "query", map[string]any{"sql": "SELECT count(*) AS n FROM customers"}, &r)
	if r.Rows[0]["n"] != float64(27) {
		t.Fatalf("header overrode none: %v", r)
	}
}

func TestManagedRawQueryUsesSelectedDatabaseAndSharedIdentity(t *testing.T) {
	ctx := context.Background()
	identity, err := store.Open(testutil.Demo(t), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer identity.Close()
	cfg, err := rewrite.LoadConfig("../../config/rewrite.yml")
	if err != nil {
		t.Fatal(err)
	}
	svc, err := service.New(ctx, identity, cfg, store.DefaultAsOf)
	if err != nil {
		t.Fatal(err)
	}
	c, err := catalog.New(filepath.Join(t.TempDir(), "databases"), identity, cfg, store.DefaultAsOf, 0)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "target.sqlite3")
	target, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = target.Exec("CREATE TABLE records(id INTEGER, secret TEXT); INSERT INTO records VALUES(1, 'test-only')")
	target.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Add(ctx, catalog.Database{ID: "target", Name: "Target", Driver: "sqlite", Path: path}); err != nil {
		t.Fatal(err)
	}
	d, err := c.Get("target")
	if err != nil {
		t.Fatal(err)
	}
	d.MCPEnabled = true
	d.Tools = []string{"raw_query"}
	if err = c.Save(ctx, *d); err != nil {
		t.Fatal(err)
	}
	connectManaged := func(uid int64) *mcp.ClientSession {
		server := ManagedServer(c, "target", svc, Options{AuthMode: "none", DefaultUserID: uid, IdentityStore: identity})
		left, right := mcp.NewInMemoryTransports()
		ss, err := server.Connect(ctx, left, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { ss.Close() })
		client := mcp.NewClient(&mcp.Implementation{Name: "managed-test", Version: "1"}, nil)
		session, err := client.Connect(ctx, right, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { session.Close() })
		return session
	}
	admin := connectManaged(175)
	if names := toolNames(t, admin); !slices.Equal(names, []string{"raw_query"}) {
		t.Fatalf("tools %v", names)
	}
	var result queryResult
	if res := call(t, admin, "raw_query", map[string]any{"sql": "SELECT * FROM records"}, &result); res.IsError || result.RowCount != 1 {
		t.Fatalf("target raw query: %+v %+v", res, result)
	}
	if res := call(t, admin, "raw_query", map[string]any{"sql": "DELETE FROM records"}, nil); !res.IsError {
		t.Fatal("raw query wrote data")
	}
	if res := call(t, connectManaged(1), "raw_query", map[string]any{"sql": "SELECT * FROM records"}, nil); !res.IsError {
		t.Fatal("ordinary employee used raw_query")
	}
	d, err = c.Get("target")
	if err != nil {
		t.Fatal(err)
	}
	d.Tools = []string{}
	if err = c.Save(ctx, *d); err != nil {
		t.Fatal(err)
	}
	if names := toolNames(t, admin); len(names) != 0 {
		t.Fatalf("revoked tools %v", names)
	}
	if _, err = admin.CallTool(ctx, &mcp.CallToolParams{Name: "raw_query", Arguments: map[string]any{"sql": "SELECT * FROM records"}}); err == nil {
		t.Fatal("revoked raw_query still callable")
	}
}

func TestManagedToolSelectionRegistersDebugTools(t *testing.T) {
	url := startServer(t, Options{Managed: true, Tools: []string{"raw_query", "customer_scope"}})
	session := connect(t, url, "", 175)
	if names := toolNames(t, session); !slices.Equal(names, []string{"customer_scope", "raw_query"}) {
		t.Fatalf("managed debug tools %v", names)
	}
	if res := call(t, session, "customer_scope", map[string]any{"user_id": 1}, nil); res.IsError {
		t.Fatalf("customer scope %v", res)
	}
}

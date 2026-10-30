package platform_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"datascope-server/internal/adminapi"
	"datascope-server/internal/guard"
	"datascope-server/internal/management"
	"datascope-server/internal/managementmcp"
	"datascope-server/internal/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func enterpriseConfig(t *testing.T, f *fixture, mode, origin string) management.AuthConfig {
	t.Helper()
	cfg, e := f.m.AuthConfig(f.ctx)
	must(t, e)
	provider := management.AuthProvider{ID: "enterprise", Name: "企业员工认证", Type: "ENTERPRISE_OAUTH", Enabled: true, Issuer: origin, AuthorizeURL: origin + "/authorize", TokenURL: origin + "/token", UserInfoURL: origin + "/userinfo", ClientID: "datascope", SubjectClaim: "sub", UsernameClaim: "username", BusinessIDClaim: "employee_id"}
	if mode == "ACCOUNT" {
		provider.Type = "ENTERPRISE_ACCOUNT_API"
		provider.AccountURL = origin + "/verify"
	}
	cfg.Providers = append(cfg.Providers, provider)
	cfg.Policies = append(cfg.Policies, management.AuthPolicy{ID: "enterprise", Name: "企业身份", Mode: mode, ProviderID: provider.ID, Permissions: []string{"query", "action"}})
	found := false
	for i, b := range cfg.Bindings {
		if b.TargetType == "DATABASE" && b.TargetID == "default" {
			cfg.Bindings[i].PolicyID = "enterprise"
			found = true
		}
	}
	if !found {
		cfg.Bindings = append(cfg.Bindings, management.AuthBinding{TargetType: "DATABASE", TargetID: "default", PolicyID: "enterprise"})
	}
	return cfg
}
func TestGlobalAuthenticationBindingsAndAllBusinessModes(t *testing.T) {
	f := setup(t)
	identity := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/verify" {
			var in map[string]string
			json.NewDecoder(r.Body).Decode(&in)
			if in["username"] != "reader" || in["password"] != "enterprise-password" {
				http.Error(w, "denied", 401)
				return
			}
		}
		if r.URL.Path == "/userinfo" && r.Header.Get("Authorization") != "Bearer enterprise-token" {
			http.Error(w, "denied", 401)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"sub": "stable-employee", "username": "reader", "employee_id": 10, "active": true, "permissions": []string{"raw_query"}})
	}))
	defer identity.Close()
	cfg := enterpriseConfig(t, f, "ACCOUNT", identity.URL)
	must(t, f.m.SaveAuthConfig(f.ctx, cfg))
	p, e := f.m.ResolveAuth(f.ctx, "DATABASE", "default")
	must(t, e)
	a, e := f.m.VerifyAccount(f.ctx, p, "default", "reader", "enterprise-password")
	must(t, e)
	if a.Username != "reader" || a.BusinessUserID != 10 || len(a.Permissions) != 2 {
		t.Fatalf("identity mapping: %+v", a)
	}
	// No BUSINESS_ACCOUNT provisioning is required for a verified employee.
	if _, e = f.m.BusinessAccount(f.ctx, "default", "reader"); e == nil {
		t.Fatal("enterprise authentication created a demo account")
	}
	h, e := mcpserver.ManagedHTTPHandler(f.c, f.st, mcpserver.Options{Management: f.m, Application: f.app}, "oauth", identity.URL, "http://127.0.0.1/mcp", "/mcp")
	must(t, e)
	server := httptest.NewServer(h)
	defer server.Close()
	req, _ := http.NewRequest("POST", server.URL+"/mcp/default", strings.NewReader(`{}`))
	res, e := http.DefaultClient.Do(req)
	must(t, e)
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatalf("missing account accepted: %d", res.StatusCode)
	}
	req, _ = http.NewRequest("POST", server.URL+"/mcp/default", strings.NewReader(`{}`))
	req.SetBasicAuth("reader", "wrong")
	res, e = http.DefaultClient.Do(req)
	must(t, e)
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatal("wrong enterprise password accepted")
	}
	basicClient := &http.Client{Transport: accountTransport{}}
	session, e := mcp.NewClient(&mcp.Implementation{Name: "auth-modes-test", Version: "1"}, nil).Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp/default", HTTPClient: basicClient, DisableStandaloneSSE: true}, nil)
	must(t, e)
	out, e := callMCP(t, session, "query", map[string]any{"sql": "SELECT id FROM items ORDER BY id"})
	must(t, e)
	raw, _ := json.Marshal(out)
	if out.IsError || !strings.Contains(string(raw), `"row_count":2`) {
		t.Fatalf("enterprise account query: %s", raw)
	}
	session.Close()
	cfg, e = f.m.AuthConfig(f.ctx)
	must(t, e)
	for i, b := range cfg.Bindings {
		if b.TargetType == "API" {
			cfg.Bindings[i].PolicyID = "enterprise"
		}
	}
	must(t, f.m.SaveAuthConfig(f.ctx, cfg))
	platform := httptest.NewServer(adminapi.PlatformHandler(f.app, http.NotFoundHandler()))
	defer platform.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	status, _ := request(t, client, "POST", platform.URL+"/api/auth/login", map[string]string{"username": "reader", "password": "enterprise-password"})
	if status != 200 {
		t.Fatalf("enterprise platform account login: %d", status)
	}
	status, _ = request(t, client, "GET", platform.URL+"/api/auth/me", nil)
	if status != 200 {
		t.Fatal("missing enterprise session")
	}
	// Changing global policy invalidates existing sessions immediately.
	cfg, e = f.m.AuthConfig(f.ctx)
	must(t, e)
	cfg.Policies = append(cfg.Policies, management.AuthPolicy{ID: "public", Name: "公开查询", Mode: "NONE", Permissions: []string{"query"}, BusinessUserID: 10})
	for i, b := range cfg.Bindings {
		if b.TargetType == "DATABASE" && b.TargetID == "default" {
			cfg.Bindings[i].PolicyID = "public"
		}
	}
	must(t, f.m.SaveAuthConfig(f.ctx, cfg))
	status, _ = request(t, client, "GET", platform.URL+"/api/auth/me", nil)
	if status != 401 {
		t.Fatal("stale session survived auth revision")
	}
	// NONE still receives an explicit policy-bound identity.
	p, e = f.m.ResolveAuth(f.ctx, "DATABASE", "default")
	must(t, e)
	if p.Policy.Mode != "NONE" || p.Policy.BusinessUserID != 10 {
		t.Fatal(p)
	}
	anonymous := connectMCP(t, server.URL+"/mcp/default", "")
	out, e = callMCP(t, anonymous, "query", map[string]any{"sql": "SELECT id FROM items ORDER BY id"})
	must(t, e)
	raw, _ = json.Marshal(out)
	if out.IsError || !strings.Contains(string(raw), `"row_count":2`) {
		t.Fatalf("anonymous fixed-scope query: %s", raw)
	}
	cfg, e = f.m.AuthConfig(f.ctx)
	must(t, e)
	for i := range cfg.Providers {
		if cfg.Providers[i].ID == "enterprise" {
			cfg.Providers[i].Type = "ENTERPRISE_OAUTH"
		}
	}
	for i := range cfg.Policies {
		if cfg.Policies[i].ID == "enterprise" {
			cfg.Policies[i].Mode = "OAUTH"
		}
	}
	for i, b := range cfg.Bindings {
		if b.TargetType == "DATABASE" && b.TargetID == "default" {
			cfg.Bindings[i].PolicyID = "enterprise"
		}
	}
	must(t, f.m.SaveAuthConfig(f.ctx, cfg))
	oauthSession := connectMCP(t, server.URL+"/mcp/default", "enterprise-token")
	out, e = callMCP(t, oauthSession, "query", map[string]any{"sql": "SELECT id FROM items ORDER BY id"})
	must(t, e)
	raw, _ = json.Marshal(out)
	if out.IsError || !strings.Contains(string(raw), `"row_count":2`) {
		t.Fatalf("enterprise OAuth query: %s", raw)
	}
	managementHandler, e := mcpserver.HTTPHandlerWithManagement(managementmcp.New(f.app), nil, "oauth", identity.URL, "http://127.0.0.1/mcp/admin", "/mcp/admin", true, f.m, "")
	must(t, e)
	managementServer := httptest.NewServer(managementHandler)
	defer managementServer.Close()
	managementSession := connectMCP(t, managementServer.URL+"/mcp/admin", "enterprise-token")
	out, e = callMCP(t, managementSession, "datascope_query", map[string]any{"action": "list", "args": map[string]any{"resourceType": "database"}})
	must(t, e)
	if out.IsError {
		t.Fatalf("enterprise management MCP rejected identity: %+v", out)
	}
	out, e = callMCP(t, managementSession, "datascope_query", map[string]any{"action": "get", "args": map[string]any{"resourceType": "authentication"}})
	if e == nil && !out.IsError {
		t.Fatal("enterprise reader escalated to system administrator")
	}
	// Invalid source/provider combinations are rejected before persistence.
	cfg, e = f.m.AuthConfig(f.ctx)
	must(t, e)
	cfg.Policies[0].Mode = "invalid"
	if e = f.m.SaveAuthConfig(f.ctx, cfg); e == nil {
		t.Fatal("invalid auth mode accepted")
	}
}
func TestEnterpriseOAuthBrowserAndActionConfirmation(t *testing.T) {
	f, _ := setupAction(t, true)
	var mu sync.Mutex
	challenge := ""
	identity := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/authorize":
			mu.Lock()
			challenge = r.URL.Query().Get("code_challenge")
			mu.Unlock()
			callback, _ := url.Parse(r.URL.Query().Get("redirect_uri"))
			q := callback.Query()
			q.Set("state", r.URL.Query().Get("state"))
			q.Set("code", "enterprise-code")
			callback.RawQuery = q.Encode()
			http.Redirect(w, r, callback.String(), 303)
		case "/token":
			r.ParseForm()
			v := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			mu.Lock()
			valid := base64.RawURLEncoding.EncodeToString(v[:]) == challenge
			mu.Unlock()
			if !valid || r.Form.Get("code") != "enterprise-code" {
				http.Error(w, "bad PKCE", 401)
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"access_token": "enterprise-token", "token_type": "Bearer"})
		case "/userinfo":
			if r.Header.Get("Authorization") != "Bearer enterprise-token" {
				http.Error(w, "denied", 401)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"sub": "stable-employee", "username": "reader", "employee_id": 10, "active": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer identity.Close()
	cfg := enterpriseConfig(t, f, "OAUTH", identity.URL)
	for i, b := range cfg.Bindings {
		if b.TargetType == "API" {
			cfg.Bindings[i].PolicyID = "enterprise"
		}
	}
	must(t, f.m.SaveAuthConfig(f.ctx, cfg))
	server := httptest.NewServer(adminapi.PlatformHandler(f.app, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("DataScope")) })))
	defer server.Close()
	f.app.ManagementMCPURL = server.URL + "/mcp/admin"

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	res, e := client.Get(server.URL + "/api/auth/enterprise/start")
	must(t, e)
	startBody, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 403 && res.StatusCode != 200 {
		t.Fatalf("OAuth browser flow: %d %s", res.StatusCode, startBody)
	}
	status, body := request(t, client, "GET", server.URL+"/api/auth/me", nil)
	if status != 200 || !strings.Contains(string(body), "reader") {
		t.Fatalf("OAuth session: %d %s", status, body)
	}
	p, e := f.m.ResolveAuth(f.ctx, "DATABASE", "default")
	must(t, e)
	a, e := p.VerifyBearer(context.Background(), "default", "enterprise-token")
	must(t, e)
	ctx := guard.WithBusiness(context.Background(), a)
	out, e := f.app.InvokeAction(ctx, "default", "rename_item", "enterprise-confirm", map[string]any{"id": 1, "name": "enterprise-confirmed"})
	must(t, e)
	if out.Status != "confirm_required" {
		t.Fatal(out)
	}
	res, e = client.Get(server.URL + "/actions/auth/start?token=" + out.ConfirmationToken)
	must(t, e)
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !strings.Contains(string(raw), "企业身份已认证") {
		t.Fatalf("action enterprise login: %d %s", res.StatusCode, raw)
	}
	endpoint, _ := url.Parse(server.URL + "/actions/confirm")
	nonce := ""
	for _, c := range jar.Cookies(endpoint) {
		if strings.HasPrefix(c.Name, "action_confirm_") {
			nonce = c.Value
		}
	}
	res, e = client.PostForm(server.URL+"/actions/confirm", url.Values{"token": {out.ConfirmationToken}, "nonce": {nonce}})
	must(t, e)
	raw, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if !strings.Contains(string(raw), "done") {
		t.Fatalf("enterprise confirmation: %s", raw)
	}
	rows, e := f.st.Query(context.Background(), "SELECT name FROM items WHERE id=1", 1)
	must(t, e)
	if rows.Rows[0]["name"] != "enterprise-confirmed" {
		t.Fatal(rows)
	}
	// The callback is bound to a browser cookie and one-time state.
	res, e = http.Get(server.URL + "/api/auth/enterprise/callback?state=" + strings.Repeat("x", 64) + "&code=enterprise-code")
	must(t, e)
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatal("unbound callback accepted")
	}
}

type accountTransport struct{}

func (accountTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.SetBasicAuth("reader", "enterprise-password")
	return http.DefaultTransport.RoundTrip(r)
}

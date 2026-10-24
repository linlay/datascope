package oauth_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"datascope-server/internal/adminapi"
	"datascope-server/internal/catalog"
	"datascope-server/internal/mcpserver"
	"datascope-server/internal/oauth"
	"datascope-server/internal/rewrite"
	"datascope-server/internal/service"
	"datascope-server/internal/store"
	"datascope-server/internal/testutil"
)

type env struct {
	as                    *oauth.Server
	issuer, resource, key string
	st                    *store.Store
	svc                   *service.QueryService
	catalog               *catalog.Catalog
}

func setup(t *testing.T) *env { return setupMode(t, false) }
func setupMode(t *testing.T, managed bool) *env {
	t.Helper()
	st, e := store.Open(testutil.Demo(t), 0)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { st.Close() })
	cfg, e := rewrite.LoadConfig("../../config/rewrite.yml")
	if e != nil {
		t.Fatal(e)
	}
	svc, e := service.New(context.Background(), st, cfg, store.DefaultAsOf)
	if e != nil {
		t.Fatal(e)
	}
	a := httptest.NewUnstartedServer(http.NotFoundHandler())
	m := httptest.NewUnstartedServer(http.NotFoundHandler())
	issuer := "http://" + a.Listener.Addr().String()
	resource := "http://" + m.Listener.Addr().String() + "/mcp"
	dir := t.TempDir()
	key := filepath.Join(dir, "signing.pem")
	var catalogue *catalog.Catalog
	var allowed func(string) bool
	if managed {
		catalogue, e = catalog.New(filepath.Join(dir, "databases"), st, cfg, store.DefaultAsOf, 0)
		if e != nil {
			t.Fatal(e)
		}
		allowed = func(target string) bool { return catalogue.AllowsMCPResource(resource, target) }
	}
	as, e := oauth.New(oauth.Config{Issuer: issuer, Resource: resource, ResourceAllowed: allowed, KeyPath: key, StatePath: filepath.Join(dir, "state.sqlite3"), Password: "test-demo-password"}, st)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { as.Close() })
	a.Config.Handler = as.Handler(http.NotFoundHandler())
	a.Start()
	t.Cleanup(a.Close)
	server := mcpserver.New(svc, mcpserver.Options{AuthMode: "oauth", Debug: true})
	if managed {
		m.Config.Handler, e = mcpserver.ManagedHTTPHandler(catalogue, st, mcpserver.Options{AuthMode: "oauth"}, "oauth", issuer, resource, "/mcp")
	} else {
		m.Config.Handler, e = mcpserver.HTTPHandler(server, st, "oauth", issuer, resource, "/mcp", true)
	}
	if e != nil {
		t.Fatal(e)
	}
	m.Start()
	t.Cleanup(m.Close)
	return &env{as: as, issuer: issuer, resource: resource, key: key, st: st, svc: svc, catalog: catalogue}
}
func request(t *testing.T, c *http.Client, method, endpoint string, form url.Values, token string) (int, http.Header, []byte) {
	t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	r, e := http.NewRequest(method, endpoint, body)
	if e != nil {
		t.Fatal(e)
	}
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	res, e := c.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return res.StatusCode, res.Header, raw
}
func register(t *testing.T, e *env) string {
	t.Helper()
	res, err := http.Post(e.issuer+"/oauth/register", "application/json", strings.NewReader(`{"client_name":"Integration test","redirect_uris":["http://127.0.0.1/callback"],"token_endpoint_auth_method":"none"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var c map[string]any
	json.NewDecoder(res.Body).Decode(&c)
	if res.StatusCode != 201 {
		t.Fatalf("register: %v", c)
	}
	return c["client_id"].(string)
}
func code(t *testing.T, e *env, id, emp string) string {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	q := url.Values{"client_id": {id}, "redirect_uri": {"http://127.0.0.1/callback"}, "response_type": {"code"}, "code_challenge_method": {"S256"}, "code_challenge": {challenge()}, "resource": {e.resource}, "scope": {"sql:query sql:debug"}, "state": {"original-state"}}
	status, headers, body := request(t, c, "GET", e.issuer+"/oauth/authorize?"+q.Encode(), nil, "")
	if status != 200 {
		t.Fatalf("authorize: %d %s", status, body)
	}
	if !strings.Contains(headers.Get("Content-Security-Policy"), "form-action 'self' http://127.0.0.1;") {
		t.Fatal("CSP blocks registered loopback callback")
	}
	if strings.Contains(string(body), "debug_employee") {
		t.Fatal("debug login present in normal mode")
	}
	u, _ := url.Parse(e.issuer + "/oauth/authorize")
	nonce := ""
	for _, cookie := range jar.Cookies(u) {
		if strings.HasPrefix(cookie.Name, "oauth_login_") {
			nonce = cookie.Value
		}
	}
	status, _, _ = request(t, c, "POST", u.String(), url.Values{"nonce": {"bad"}, "emp_no": {emp}, "password": {"test-demo-password"}}, "")
	if status != 400 {
		t.Fatal("missing CSRF accepted")
	}
	status, _, _ = request(t, c, "POST", u.String(), url.Values{"nonce": {nonce}, "emp_no": {emp}, "password": {"wrong"}}, "")
	if status != 401 {
		t.Fatal("wrong password accepted")
	}
	status, h, body := request(t, c, "POST", u.String(), url.Values{"nonce": {nonce}, "emp_no": {emp}, "password": {"test-demo-password"}}, "")
	if emp == "E0176" {
		if status != 401 {
			t.Fatal("resigned login accepted")
		}
		return ""
	}
	if status != 303 {
		t.Fatalf("login: %d %s", status, body)
	}
	dest, _ := url.Parse(h.Get("Location"))
	if dest.Query().Get("state") != "original-state" || dest.Query().Get("iss") != e.issuer {
		t.Fatal("state/issuer lost")
	}
	return dest.Query().Get("code")
}

const verifier = "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"

func challenge() string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}
func exchange(t *testing.T, e *env, form url.Values, want int) map[string]any {
	t.Helper()
	status, headers, raw := request(t, http.DefaultClient, "POST", e.issuer+"/oauth/token", form, "")
	if status != want {
		t.Fatalf("token %d want %d: %s", status, want, raw)
	}
	if headers.Get("Cache-Control") != "no-store" {
		t.Fatal("token cached")
	}
	var v map[string]any
	json.Unmarshal(raw, &v)
	return v
}
func TestOAuthPKCERefreshAndMCP(t *testing.T) {
	e := setup(t)
	id := register(t, e)
	authCode := code(t, e, id, "E0001")
	form := url.Values{"grant_type": {"authorization_code"}, "client_id": {id}, "redirect_uri": {"http://127.0.0.1/callback"}, "code": {authCode}, "code_verifier": {verifier}, "resource": {e.resource}}
	form.Set("code_verifier", strings.Repeat("x", 43))
	exchange(t, e, form, 400)
	form.Set("code_verifier", verifier)
	form.Set("resource", e.issuer)
	exchange(t, e, form, 400)
	form.Set("resource", e.resource)
	tokens := exchange(t, e, form, 200)
	if tokens["scope"] != "sql:query" {
		t.Fatal("employee got debug scope")
	}
	exchange(t, e, form, 400)
	token := tokens["access_token"].(string)
	cs := connect(t, e, token)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "query", Arguments: map[string]any{"sql": "SELECT COUNT(*) AS n FROM customers"}})
	if err != nil || res.IsError {
		t.Fatalf("query: %v %v", res, err)
	}
	raw, _ := json.Marshal(res.StructuredContent)
	if !strings.Contains(string(raw), `"n":27`) {
		t.Fatalf("scope wrong: %s", raw)
	}
	res, err = cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "raw_query", Arguments: map[string]any{"sql": "SELECT * FROM customers"}})
	if err != nil || !res.IsError {
		t.Fatal("ordinary OAuth user bypassed raw_query")
	}
	refresh := url.Values{"grant_type": {"refresh_token"}, "client_id": {id}, "refresh_token": {tokens["refresh_token"].(string)}, "resource": {e.resource}}
	rotated := exchange(t, e, refresh, 200)
	exchange(t, e, refresh, 400)
	refresh.Set("refresh_token", rotated["refresh_token"].(string))
	exchange(t, e, refresh, 400)
	// Administrator's regular query token cannot gain debug unless requested.
	ac := code(t, e, id, "E0175")
	form.Set("code", ac)
	admin := exchange(t, e, form, 200)
	if !strings.Contains(admin["scope"].(string), "sql:debug") {
		t.Fatal("admin debug missing")
	}
	acs := connect(t, e, admin["access_token"].(string))
	res, err = acs.CallTool(context.Background(), &mcp.CallToolParams{Name: "raw_query", Arguments: map[string]any{"sql": "SELECT COUNT(*) AS n FROM customers"}})
	if err != nil || res.IsError {
		t.Fatal("admin raw query denied")
	}
	code(t, e, id, "E0176")
	in := &adminapi.Inspector{Endpoint: e.resource, Service: e.svc, FixturesDir: "../../fixtures", Token: e.as.SignEmployee}
	report, err := in.Verify(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed != 0 {
		for _, c := range report.Checks {
			if c.Status == "fail" {
				t.Errorf("%s %s: %s expected %v got %v", c.Group, c.Name, c.Detail, c.Expected, c.Actual)
			}
		}
	}
}

type bearerTransport struct{ token string }

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	r.Header.Set("X-User-Id", "175")
	return http.DefaultTransport.RoundTrip(r)
}
func connect(t *testing.T, e *env, token string) *mcp.ClientSession {
	t.Helper()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: e.resource, HTTPClient: &http.Client{Transport: bearerTransport{token: token}}, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}
func TestDiscoveryAndJWTValidation(t *testing.T) {
	e := setup(t)
	status, h, _ := request(t, http.DefaultClient, "POST", e.resource, nil, "")
	if status != 401 || !strings.Contains(h.Get("WWW-Authenticate"), "resource_metadata=") {
		t.Fatalf("challenge %d %v", status, h)
	}
	for _, u := range []string{e.issuer + "/.well-known/oauth-authorization-server", strings.TrimSuffix(e.resource, "/mcp") + "/.well-known/oauth-protected-resource/mcp"} {
		status, _, raw := request(t, http.DefaultClient, "GET", u, nil, "")
		if status != 200 {
			t.Fatalf("metadata %d %s", status, raw)
		}
	}
	raw, _ := os.ReadFile(e.key)
	block, _ := pem.Decode(raw)
	key, _ := x509.ParsePKCS1PrivateKey(block.Bytes)
	forge := func(issuer, aud, sub, scope string, exp int64) string {
		head := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"at+jwt","kid":"demo-rsa-1"}`))
		claims, _ := json.Marshal(map[string]any{"iss": issuer, "aud": aud, "sub": sub, "scope": scope, "exp": exp, "iat": time.Now().Unix()})
		body := head + "." + base64.RawURLEncoding.EncodeToString(claims)
		h := sha256.Sum256([]byte(body))
		sig, _ := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, h[:])
		return body + "." + base64.RawURLEncoding.EncodeToString(sig)
	}
	for _, token := range []string{"bad", forge(e.issuer, e.resource, "E0001", "sql:query", time.Now().Add(-time.Second).Unix()), forge(e.issuer, "other", "E0001", "sql:query", time.Now().Add(time.Hour).Unix()), forge("other", e.resource, "E0001", "sql:query", time.Now().Add(time.Hour).Unix()), forge(e.issuer, e.resource, "E0176", "sql:query", time.Now().Add(time.Hour).Unix()), forge(e.issuer, e.resource, "E0001", "sql:query sql:debug", time.Now().Add(time.Hour).Unix())} {
		status, _, _ := request(t, http.DefaultClient, "POST", e.resource, nil, token)
		if status != 401 {
			t.Fatalf("invalid JWT status %d", status)
		}
	}
	token, _ := e.as.SignEmployee(context.Background(), 1)
	parts := strings.Split(token, ".")
	parts[1] = base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"E0175"}`))
	status, _, _ = request(t, http.DefaultClient, "POST", e.resource, nil, strings.Join(parts, "."))
	if status != 401 {
		t.Fatal("tampered JWT accepted")
	}
	id := register(t, e)
	q := url.Values{"client_id": {id}, "redirect_uri": {"http://127.0.0.1/attacker"}}
	status, headers, _ := request(t, http.DefaultClient, "GET", e.issuer+"/oauth/authorize?"+q.Encode(), nil, "")
	if status != 400 || headers.Get("Location") != "" {
		t.Fatal("unregistered redirect accepted")
	}
}

func TestConcurrentAuthorizationPages(t *testing.T) {
	e := setup(t)
	id := register(t, e)
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	q := url.Values{"client_id": {id}, "redirect_uri": {"http://127.0.0.1/callback"}, "response_type": {"code"}, "code_challenge_method": {"S256"}, "code_challenge": {challenge()}, "resource": {e.resource}, "scope": {"sql:query"}}
	nonceRE := regexp.MustCompile(`name="nonce" value="([^"]+)"`)
	nonces := []string{}
	for _, state := range []string{"tab-a", "tab-b"} {
		q.Set("state", state)
		status, _, body := request(t, c, "GET", e.issuer+"/oauth/authorize?"+q.Encode(), nil, "")
		if status != 200 {
			t.Fatalf("authorize %d %s", status, body)
		}
		m := nonceRE.FindSubmatch(body)
		if len(m) != 2 {
			t.Fatal("missing nonce")
		}
		nonces = append(nonces, string(m[1]))
	}
	for i, nonce := range nonces {
		status, h, body := request(t, c, "POST", e.issuer+"/oauth/authorize", url.Values{"nonce": {nonce}, "emp_no": {"E0001"}, "password": {"test-demo-password"}}, "")
		if status != 303 {
			t.Fatalf("tab %d login failed: %d %s", i, status, body)
		}
		dest, _ := url.Parse(h.Get("Location"))
		if dest.Query().Get("state") != []string{"tab-a", "tab-b"}[i] {
			t.Fatal("tab state mixed")
		}
	}
}

func TestLoginRequiresCookieAndReportsStalePage(t *testing.T) {
	e := setup(t)
	id := register(t, e)
	q := url.Values{"client_id": {id}, "redirect_uri": {"http://127.0.0.1/callback"}, "response_type": {"code"}, "code_challenge_method": {"S256"}, "code_challenge": {challenge()}, "resource": {e.resource}}
	endpoint := e.issuer + "/oauth/authorize?" + q.Encode()
	status, _, body := request(t, http.DefaultClient, "GET", endpoint, nil, "")
	if status != 200 {
		t.Fatal("authorize failed")
	}
	m := regexp.MustCompile(`name="nonce" value="([^"]+)"`).FindSubmatch(body)
	if len(m) != 2 {
		t.Fatal("no nonce")
	}
	status, h, body := request(t, http.DefaultClient, "POST", endpoint, url.Values{"nonce": {string(m[1])}, "emp_no": {"E0001"}, "password": {"test-demo-password"}}, "")
	if status != 400 || h.Get("Location") != "" || !strings.Contains(string(body), "登录会话已失效") || !strings.Contains(string(body), "重新打开授权页") {
		t.Fatalf("missing cookie accepted or unclear response: %d %s", status, body)
	}
	if strings.Contains(string(body), "test-demo-password") {
		t.Fatal("password reflected")
	}
}

func TestManagedEndpointOAuthDiscoveryLoginAndAudience(t *testing.T) {
	e := setupMode(t, true)
	id := register(t, e)
	if err := e.catalog.Add(context.Background(), catalog.Database{ID: "other", Name: "Other", Driver: "sqlite", Path: e.st.Path()}); err != nil {
		t.Fatal(err)
	}
	other, err := e.catalog.Get("other")
	if err != nil {
		t.Fatal(err)
	}
	other.MCPEnabled = true
	other.Tools = []string{"list_tables"}
	if err = e.catalog.Save(context.Background(), *other); err != nil {
		t.Fatal(err)
	}
	origin := strings.TrimSuffix(e.resource, "/mcp")
	var previousToken, lastRefresh string
	for _, suffix := range []string{"", "/default", "/finance"} {
		t.Run("resource"+suffix, func(t *testing.T) {
			if suffix == "/finance" {
				d, err := e.catalog.Get("default")
				if err != nil {
					t.Fatal(err)
				}
				d.MCPPath = "finance"
				if err = e.catalog.Save(context.Background(), *d); err != nil {
					t.Fatal(err)
				}
				status, _, _ := request(t, http.DefaultClient, "POST", e.resource+"/default", nil, "")
				if status != 404 {
					t.Fatalf("old route status %d", status)
				}
			}
			endpoint := e.resource + suffix
			status, h, _ := request(t, http.DefaultClient, "POST", endpoint, nil, "")
			if status != 401 {
				t.Fatalf("challenge status %d", status)
			}
			challenges, err := oauthex.ParseWWWAuthenticate(h.Values("WWW-Authenticate"))
			if err != nil {
				t.Fatal(err)
			}
			metadataURL := ""
			for _, challenge := range challenges {
				if strings.EqualFold(challenge.Scheme, "Bearer") {
					metadataURL = challenge.Params["resource_metadata"]
				}
			}
			expectedMetadata := origin + "/.well-known/oauth-protected-resource/mcp" + suffix
			if metadataURL != expectedMetadata {
				t.Fatalf("metadata pointer %q want %q", metadataURL, expectedMetadata)
			}
			// This is the SDK's exact resource check that Platform reported failing.
			metadata, err := oauthex.GetProtectedResourceMetadata(context.Background(), metadataURL, endpoint, http.DefaultClient)
			if err != nil {
				t.Fatal(err)
			}
			if metadata.Resource != endpoint {
				t.Fatal("resource mismatch")
			}
			scoped := *e
			scoped.resource = endpoint
			authCode := code(t, &scoped, id, "E0001")
			form := url.Values{"grant_type": {"authorization_code"}, "client_id": {id}, "redirect_uri": {"http://127.0.0.1/callback"}, "code": {authCode}, "code_verifier": {verifier}, "resource": {endpoint}}
			wrong := url.Values{}
			for key, values := range form {
				wrong[key] = values
			}
			wrong.Set("resource", e.resource+"/other")
			exchange(t, e, wrong, 400)
			tokens := exchange(t, e, form, 200)
			token := tokens["access_token"].(string)
			claimsRaw, err := base64.RawURLEncoding.DecodeString(strings.Split(token, ".")[1])
			if err != nil {
				t.Fatal(err)
			}
			var claims struct{ Aud string }
			if err = json.Unmarshal(claimsRaw, &claims); err != nil {
				t.Fatal(err)
			}
			if claims.Aud != endpoint {
				t.Fatalf("audience %q want %q", claims.Aud, endpoint)
			}
			session := connect(t, &scoped, token)
			if tools, err := session.ListTools(context.Background(), nil); err != nil || len(tools.Tools) != 3 {
				t.Fatalf("tools %v %v", tools, err)
			}
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "query", Arguments: map[string]any{"sql": "SELECT COUNT(*) AS n FROM customers"}})
			if err != nil || result.IsError {
				t.Fatalf("query %v %v", result, err)
			}
			// Correctly signed tokens for another database or the root alias cannot
			// be replayed at this endpoint.
			status, _, _ = request(t, http.DefaultClient, "POST", e.resource+"/other", nil, token)
			if status != 401 {
				t.Fatalf("cross-resource token accepted: %d", status)
			}
			if previousToken != "" {
				status, _, _ = request(t, http.DefaultClient, "POST", endpoint, nil, previousToken)
				if status != 401 {
					t.Fatalf("previous audience accepted: %d", status)
				}
			}
			refresh := url.Values{"grant_type": {"refresh_token"}, "client_id": {id}, "refresh_token": {tokens["refresh_token"].(string)}, "resource": {endpoint}}
			refresh.Set("resource", e.resource+"/other")
			exchange(t, e, refresh, 400)
			refresh.Set("resource", endpoint)
			rotated := exchange(t, e, refresh, 200)
			verified := &oauth.Verifier{Issuer: e.issuer, Resource: endpoint, Store: e.st}
			if _, err := verified.Verify(context.Background(), rotated["access_token"].(string), nil); err != nil {
				t.Fatal("refreshed token audience invalid", err)
			}
			previousToken = token
			lastRefresh = rotated["refresh_token"].(string)
		})
	}
	// The management inspector must request the same audience as the scoped URL.
	in := &adminapi.Inspector{Endpoint: e.resource, Service: e.svc, Token: e.as.SignEmployee}
	in.TokenForDatabase = func(ctx context.Context, userID int64, databaseID string) (string, error) {
		d, err := e.catalog.Get(databaseID)
		if err != nil {
			return "", err
		}
		return e.as.SignEmployeeForResource(ctx, userID, e.resource+"/"+d.MCPRoute())
	}
	w := httptest.NewRecorder()
	adminapi.ManagedHandler(e.catalog, in).ServeHTTP(w, httptest.NewRequest("GET", "/api/databases/default/mcp/info", nil))
	if w.Code != 200 {
		t.Fatalf("scoped inspector %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), e.resource+"/finance") {
		t.Fatal("inspector used old resource")
	}
	// A second database receives its own usable token and its own tool selection.
	otherToken, err := e.as.SignEmployeeForResource(context.Background(), 1, e.resource+"/other")
	if err != nil {
		t.Fatal(err)
	}
	otherEnv := *e
	otherEnv.resource += "/other"
	if tools, err := connect(t, &otherEnv, otherToken).ListTools(context.Background(), nil); err != nil || len(tools.Tools) != 1 {
		t.Fatalf("second database tools %v %v", tools, err)
	}
	if status, _, _ := request(t, http.DefaultClient, "POST", e.resource+"/finance", nil, otherToken); status != 401 {
		t.Fatalf("second database token accepted by first: %d", status)
	}
	// The authorization server only accepts exact active endpoints on the
	// configured origin, never arbitrary descendants or caller-supplied hosts.
	for _, target := range []string{"https://attacker.example/mcp/finance", e.resource + "/not-configured", e.resource + "/default", e.resource + "/finance/extra"} {
		q := url.Values{"client_id": {id}, "redirect_uri": {"http://127.0.0.1/callback"}, "response_type": {"code"}, "code_challenge_method": {"S256"}, "code_challenge": {challenge()}, "resource": {target}}
		status, _, body := request(t, http.DefaultClient, "GET", e.issuer+"/oauth/authorize?"+q.Encode(), nil, "")
		if status != 400 || !strings.Contains(string(body), "invalid_target") {
			t.Fatalf("target accepted %q: %d", target, status)
		}
	}
	// Disabling this database prevents new tokens and refresh without affecting others.
	d, err := e.catalog.Get("default")
	if err != nil {
		t.Fatal(err)
	}
	d.MCPEnabled = false
	if err = e.catalog.Save(context.Background(), *d); err != nil {
		t.Fatal(err)
	}
	if _, err = e.as.SignEmployeeForResource(context.Background(), 1, e.resource+"/finance"); err == nil {
		t.Fatal("disabled database received a token")
	}
	exchange(t, e, url.Values{"grant_type": {"refresh_token"}, "client_id": {id}, "refresh_token": {lastRefresh}, "resource": {e.resource + "/finance"}}, 400)
	if _, err = e.as.SignEmployeeForResource(context.Background(), 1, e.resource+"/other"); err != nil {
		t.Fatal("disabling one database affected another", err)
	}

}

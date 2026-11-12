package platform_test

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
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"datascope-server/internal/catalog"
	"datascope-server/internal/guard"
	"datascope-server/internal/management"
	"datascope-server/internal/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func oneIDFixture(t *testing.T) (management.AuthProvider, func(map[string]any, string) string) {
	p, sign, _ := oneIDFixtureKeys(t)
	return p, sign
}

func oneIDFixtureKeys(t *testing.T) (management.AuthProvider, func(map[string]any, string) string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	must(t, err)
	b64 := base64.RawURLEncoding.EncodeToString
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{"kty": "RSA", "kid": "test-key", "alg": "RS256", "use": "sig", "n": b64(key.N.Bytes()), "e": b64(big.NewInt(int64(key.E)).Bytes())}}})
	}))
	t.Cleanup(server.Close)
	p := management.AuthProvider{ID: "oneid", Name: "Desktop OneID", Type: "ONEID", Enabled: true, Issuer: server.URL, JWKSURL: server.URL + "/jwks", Audience: "desktop", SubjectClaim: "sub", UsernameClaim: "username", BusinessIDClaim: "employee_id"}
	sign := func(patch map[string]any, algorithm string) string {
		t.Helper()
		claims := map[string]any{"iss": p.Issuer, "aud": p.Audience, "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Add(-time.Second).Unix(), "sub": "stable-employee", "username": "employee", "employee_id": 10, "permissions": []string{"raw_query", "action"}}
		for k, v := range patch {
			if v == nil {
				delete(claims, k)
			} else {
				claims[k] = v
			}
		}
		header, _ := json.Marshal(map[string]string{"alg": algorithm, "kid": "test-key"})
		body, _ := json.Marshal(claims)
		unsigned := b64(header) + "." + b64(body)
		digest := sha256.Sum256([]byte(unsigned))
		sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
		must(t, err)
		return unsigned + "." + b64(sig)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	must(t, err)
	return p, sign, string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}))
}

func TestOneIDValidation(t *testing.T) {
	p, sign := oneIDFixture(t)
	r := management.ResolvedAuth{Provider: p, Source: "DATABASE", Policy: management.AuthPolicy{Mode: "ONEID", Permissions: []string{"query"}}}
	for _, tc := range []struct {
		name      string
		patch     map[string]any
		algorithm string
	}{
		{"issuer", map[string]any{"iss": "https://attacker.example"}, "RS256"},
		{"audience", map[string]any{"aud": "other-app"}, "RS256"},
		{"expiration", map[string]any{"exp": time.Now().Add(-time.Second).Unix()}, "RS256"},
		{"missing expiration", map[string]any{"exp": nil}, "RS256"},
		{"invalid expiration type", map[string]any{"exp": "9999999999"}, "RS256"},
		{"not yet valid", map[string]any{"nbf": time.Now().Add(time.Hour).Unix()}, "RS256"},
		{"future issuance", map[string]any{"iat": time.Now().Add(time.Hour).Unix()}, "RS256"},
		{"subject", map[string]any{"sub": nil}, "RS256"},
		{"missing business identity", map[string]any{"employee_id": nil}, "RS256"},
		{"inactive", map[string]any{"active": false}, "RS256"},
		{"multiple audiences require azp", map[string]any{"aud": []string{"desktop", "other"}}, "RS256"},
		{"algorithm", nil, "HS256"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := r.VerifyBearer(context.Background(), "default", sign(tc.patch, tc.algorithm)); err == nil {
				t.Fatal("invalid ID token accepted")
			}
		})
	}
	for _, aud := range []any{p.Audience, []string{p.Audience}, []string{p.Audience, "other"}} {
		a, err := r.VerifyBearer(context.Background(), "default", sign(map[string]any{"aud": aud, "azp": p.Audience}, "RS256"))
		must(t, err)
		if a.BusinessUserID != 10 || a.Username != "employee" || len(a.Permissions) != 1 || a.Permissions[0] != "query" {
			t.Fatalf("untrusted identity/permissions: %+v", a)
		}
	}
	token := sign(nil, "RS256")
	parts := strings.Split(token, ".")
	parts[1] = base64.RawURLEncoding.EncodeToString([]byte(`{"employee_id":20}`))
	if _, err := r.VerifyBearer(context.Background(), "default", strings.Join(parts, ".")); err == nil {
		t.Fatal("tampered token accepted")
	}
	if _, err := r.VerifyBearer(context.Background(), "default", ""); err == nil {
		t.Fatal("empty token accepted")
	}
}

func TestConnectorOneIDAndTokenMCP(t *testing.T) {
	f := setup(t)
	p, sign, publicPEM := oneIDFixtureKeys(t)
	const secret = "test-api-key-at-least-sixteen-characters"
	t.Setenv("DATASCOPE_AUTH_CONNECTOR_KEY", secret)
	configure := func(mode string, provider management.AuthProvider) {
		t.Helper()
		d, err := f.c.Get("default")
		must(t, err)
		d.Authentication = &catalog.Authentication{Type: "local", Source: "DATABASE", Provider: &provider, Access: &management.AuthPolicy{ID: "connector", Name: "Connector auth", Mode: mode, ProviderID: provider.ID, Permissions: []string{"query"}, BusinessUserID: 10}}
		must(t, f.c.Save(f.ctx, *d))
	}
	h, err := mcpserver.ManagedHTTPHandler(f.c, f.st, mcpserver.Options{Management: f.m, Application: f.app}, "oauth", p.Issuer, p.Issuer+"/mcp", "/mcp")
	must(t, err)
	server := httptest.NewServer(h)
	defer server.Close()
	probe := func(header, value string, want int) http.Header {
		t.Helper()
		req, _ := http.NewRequest("POST", server.URL+"/mcp/default", strings.NewReader(`{}`))
		if header != "" {
			req.Header.Set(header, value)
		}
		res, err := http.DefaultClient.Do(req)
		must(t, err)
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
		if res.StatusCode != want {
			t.Fatalf("status = %d, want %d", res.StatusCode, want)
		}
		return res.Header
	}
	query := func(token string) {
		t.Helper()
		session := connectMCP(t, server.URL+"/mcp/default", token)
		defer session.Close()
		out, err := callMCP(t, session, "query", map[string]any{"sql": "SELECT id FROM items ORDER BY id"})
		must(t, err)
		raw, _ := json.Marshal(out)
		if out.IsError || !strings.Contains(string(raw), `"row_count":2`) {
			t.Fatalf("wrong identity/query: %s", raw)
		}
	}
	configure("ONEID", p)
	headers := probe("", "", 401)
	if headers.Get("WWW-Authenticate") != `Bearer realm="DataScope"` || headers.Get("Location") != "" {
		t.Fatalf("OneID must request the client's ID token without SSO discovery or redirect: %v", headers)
	}
	probe("Authorization", "Bearer wrong", 401)
	query(sign(nil, "RS256"))
	res, err := http.Get(server.URL + "/.well-known/oauth-protected-resource/mcp/default")
	must(t, err)
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if strings.Contains(string(raw), "authorization_servers") {
		t.Fatal("oneid advertised browser OAuth")
	}
	// Pin a PEM key and use the numeric staff ID directly.
	pinned := p
	pinned.JWKSURL = ""
	pinned.PublicKeyPEM = publicPEM
	pinned.BusinessIDClaim = "staffno"
	configure("ONEID", pinned)
	d, err := f.c.Get("default")
	must(t, err)
	query(sign(map[string]any{"staffno": "10", "name": "Sales demo"}, "RS256"))
	probe("Authorization", "Bearer "+sign(map[string]any{"staffno": "unmapped"}, "RS256"), 401)
	// Switching back to JWKS preserves the same direct identity and scoped result.
	d, err = f.c.Get("default")
	must(t, err)
	p.BusinessIDClaim = "staffno"
	d.Authentication.Provider = &p
	must(t, f.c.Save(f.ctx, *d))
	query(sign(map[string]any{"staffno": "10"}, "RS256"))
	key := management.AuthProvider{ID: "key", Name: "API key", Type: "STATIC_TOKEN", Enabled: true, SecretEnv: "DATASCOPE_AUTH_CONNECTOR_KEY"}
	configure("TOKEN", key)
	probe("", "", 401)
	probe("Authorization", "Bearer wrong", 401)
	probe("Authorization", "Bearer "+sign(nil, "RS256"), 401)
	query(secret)
	key.HeaderName = "X-API-Key"
	configure("TOKEN", key)
	probe("Authorization", "Bearer "+secret, 401)
	// Valid key passes authentication; malformed protocol input returns 400.
	probe("X-API-Key", secret, 415)
	keySession, err := mcp.NewClient(&mcp.Implementation{Name: "api-key-test", Version: "1"}, nil).Connect(f.ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp/default", HTTPClient: &http.Client{Transport: connectorKeyTransport{secret}}, DisableStandaloneSSE: true}, nil)
	must(t, err)
	keyResult, err := callMCP(t, keySession, "query", map[string]any{"sql": "SELECT id FROM items ORDER BY id"})
	must(t, err)
	keyRaw, _ := json.Marshal(keyResult)
	if keyResult.IsError || !strings.Contains(string(keyRaw), `"row_count":2`) {
		t.Fatalf("API key query: %s", keyRaw)
	}
	keySession.Close()
	key.SecretEnv = "DATASCOPE_AUTH_MISSING_KEY"
	configure("TOKEN", key)
	probe("X-API-Key", secret, 401)
	probe("", "", 401)
	for _, header := range []string{"Authorization", "Cookie", "Host", "Mcp-Session-Id", "X-Key\r\nInjected"} {
		key.HeaderName = header
		d, err := f.c.Get("default")
		must(t, err)
		d.Authentication.Provider = &key
		if err := f.c.Save(f.ctx, *d); err == nil {
			t.Fatalf("invalid API key header accepted: %q", header)
		}
	}
	// New database-only modes must never replace platform login policy.
	cfg, err := f.m.AuthConfig(f.ctx)
	must(t, err)
	cfg.Providers = append(cfg.Providers, p)
	cfg.Policies = append(cfg.Policies, management.AuthPolicy{ID: "sso", Name: "SSO", Mode: "ONEID", ProviderID: p.ID})
	for i := range cfg.Bindings {
		if cfg.Bindings[i].TargetType == "API" {
			cfg.Bindings[i].PolicyID = "sso"
		}
	}
	if err := f.m.SaveAuthConfig(f.ctx, cfg); err == nil {
		t.Fatal("OneID replaced platform account authentication")
	}
}

type connectorKeyTransport struct{ secret string }

func (t connectorKeyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("X-API-Key", t.secret)
	return http.DefaultTransport.RoundTrip(r)
}

func TestOneIDAndSharedTokenActionConfirmation(t *testing.T) {
	f, _ := setupAction(t, true)
	p, sign := oneIDFixture(t)
	d, err := f.c.Get("default")
	must(t, err)
	d.Authentication = &catalog.Authentication{Type: "local", Source: "DATABASE", Provider: &p, Access: &management.AuthPolicy{ID: "sso", Name: "SSO", Mode: "ONEID", ProviderID: p.ID, Permissions: []string{"query", "action"}}}
	must(t, f.c.Save(f.ctx, *d))
	resolved, err := f.m.ResolveAuth(f.ctx, "DATABASE", "default")
	must(t, err)
	token := sign(nil, "RS256")
	a, err := resolved.VerifyBearer(f.ctx, "default", token)
	must(t, err)
	out, err := f.app.InvokeAction(guard.WithBusiness(f.ctx, a), "default", "rename_item", "oneid-confirm", map[string]any{"id": 1, "name": "SSO confirmed"})
	must(t, err)
	if out.Status != "confirm_required" {
		t.Fatalf("preview: %+v", out)
	}
	if _, err := f.app.ConfirmAction(f.ctx, out.ConfirmationToken, sign(map[string]any{"sub": "someone-else"}, "RS256")); err == nil {
		t.Fatal("different person confirmed action")
	}
	_, err = f.app.ConfirmAction(f.ctx, out.ConfirmationToken, token)
	must(t, err)
	key := management.AuthProvider{ID: "key", Name: "Service key", Type: "STATIC_TOKEN", Enabled: true, SecretEnv: "DATASCOPE_AUTH_CONFIRM_KEY"}
	t.Setenv(key.SecretEnv, "service-key-at-least-sixteen-characters")
	d, err = f.c.Get("default")
	must(t, err)
	d.Authentication.Provider = &key
	d.Authentication.Access = &management.AuthPolicy{ID: "service", Name: "Service", Mode: "TOKEN", ProviderID: key.ID, Permissions: []string{"query", "action"}, BusinessUserID: 10}
	must(t, f.c.Save(f.ctx, *d))
	resolved, err = f.m.ResolveAuth(f.ctx, "DATABASE", "default")
	must(t, err)
	a, err = resolved.VerifyBearer(f.ctx, "default", "service-key-at-least-sixteen-characters")
	must(t, err)
	out, err = f.app.InvokeAction(guard.WithBusiness(f.ctx, a), "default", "rename_item", "key-confirm", map[string]any{"id": 1, "name": "cannot confirm"})
	must(t, err)
	if _, err := f.app.ConfirmAction(guard.WithBusiness(f.ctx, a), out.ConfirmationToken, "service-key-at-least-sixteen-characters"); err == nil {
		t.Fatal("shared key substituted for personal confirmation")
	}
}

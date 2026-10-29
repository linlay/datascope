package platform_test

import (
	"context"
	"crypto/sha256"
	"datascope-server/internal/oauth"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestPlatformOAuthLoginCodeAndRefresh(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	host := httptest.NewUnstartedServer(http.NotFoundHandler())
	issuer := "http://" + host.Listener.Addr().String()
	resource := issuer + "/mcp/admin"
	as, e := oauth.New(oauth.Config{Management: f.m, Issuer: issuer, Resource: resource, KeyPath: filepath.Join(f.dir, "signing.pem"), StatePath: f.m.Path, ResourceAllowed: func(r string) bool { return r == resource }, DatabaseForResource: func(string) (string, error) { return "", nil }}, nil)
	must(t, e)
	defer as.Close()
	host.Config.Handler = as.Handler(http.NotFoundHandler())
	host.Start()
	defer host.Close()
	status, raw := request(t, http.DefaultClient, "POST", issuer+"/oauth/register", map[string]any{"redirect_uris": []string{"http://127.0.0.1:19999/callback"}, "client_name": "platform-flow", "token_endpoint_auth_method": "none"})
	if status != 201 {
		t.Fatalf("registration %d %s", status, raw)
	}
	var client struct {
		ID string `json:"client_id"`
	}
	must(t, json.Unmarshal(raw, &client))
	verifier := strings.Repeat("a", 43)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	q := url.Values{"response_type": {"code"}, "client_id": {client.ID}, "redirect_uri": {"http://127.0.0.1:19999/callback"}, "resource": {resource}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}, "scope": {"sql:query"}, "state": {"state"}}
	jar, e := cookiejar.New(nil)
	must(t, e)
	browser := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, e := browser.Get(issuer + "/oauth/authorize?" + q.Encode())
	must(t, e)
	body, e := io.ReadAll(res.Body)
	res.Body.Close()
	must(t, e)
	if res.StatusCode != 200 {
		t.Fatalf("authorize %d %s", res.StatusCode, body)
	}
	matches := regexp.MustCompile(`name="nonce" value="([^"]+)"`).FindSubmatch(body)
	if len(matches) != 2 {
		t.Fatalf("missing login nonce: %s", body)
	}
	// Platform passwords are checked independently of business employee tables.
	res, e = browser.PostForm(issuer+"/oauth/authorize", url.Values{"nonce": {string(matches[1])}, "emp_no": {"reader"}, "password": {"reader-test-password"}})
	must(t, e)
	res.Body.Close()
	if res.StatusCode != 303 {
		t.Fatalf("login %d", res.StatusCode)
	}
	callback, e := url.Parse(res.Header.Get("Location"))
	must(t, e)
	code := callback.Query().Get("code")
	if code == "" {
		t.Fatal("missing code")
	}
	exchange := func(values url.Values) (int, []byte) {
		res, e := browser.PostForm(issuer+"/oauth/token", values)
		must(t, e)
		defer res.Body.Close()
		b, e := io.ReadAll(res.Body)
		must(t, e)
		return res.StatusCode, b
	}
	status, raw = exchange(url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {client.ID}, "redirect_uri": {"http://127.0.0.1:19999/callback"}, "resource": {resource}, "code_verifier": {verifier}})
	if status != 200 {
		t.Fatalf("exchange %d %s", status, raw)
	}
	var tokens struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
	}
	must(t, json.Unmarshal(raw, &tokens))
	v := &oauth.Verifier{Issuer: issuer, Resource: resource, Management: f.m}
	_, e = v.Verify(ctx, tokens.Access, nil)
	must(t, e)
	status, raw = exchange(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tokens.Refresh}, "client_id": {client.ID}, "resource": {resource}})
	if status != 200 {
		t.Fatalf("refresh %d %s", status, raw)
	}
	must(t, json.Unmarshal(raw, &tokens))
	must(t, f.m.ResetPassword(f.ctx, f.reader.ID, "reader-reset-password"))
	status, _ = exchange(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tokens.Refresh}, "client_id": {client.ID}, "resource": {resource}})
	if status != 400 {
		t.Fatalf("old refresh after reset %d", status)
	}
	must(t, f.m.DisableUser(f.ctx, f.reader.ID))
	_, e = f.m.Authenticate(ctx, "reader", "reader-reset-password")
	if e == nil {
		t.Fatal("disabled user authenticated")
	}
	must(t, f.m.EnableUser(f.ctx, f.reader.ID))
	_, e = f.m.Authenticate(ctx, "reader", "reader-reset-password")
	must(t, e)
}

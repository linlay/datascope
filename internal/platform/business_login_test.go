package platform_test

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"datascope-server/internal/oauth"
)

func TestPublicBusinessLoginInfo(t *testing.T) {
	f, server := setupWorkbenchProxy(t, "ACCOUNT")
	response, err := http.Get(server.URL + "/auth/databases/default/login-info")
	must(t, err)
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	must(t, err)
	if response.StatusCode != 200 || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("login info status/cache: %d %s", response.StatusCode, raw)
	}
	var info struct {
		ID       string `json:"database_id"`
		Name     string `json:"name"`
		Endpoint string `json:"mcp_endpoint"`
		Expires  int    `json:"expires_in"`
	}
	must(t, json.Unmarshal(raw, &info))
	if info.ID != "default" || info.Name == "" || info.Endpoint != f.app.MCPBase+"/default" || info.Expires != 3600 {
		t.Fatalf("invalid public details: %s", raw)
	}
	for _, secret := range []string{"password", "secret_env", "accounts", "provider", "permissions"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("exposed private configuration: %s", secret)
		}
	}
	for _, path := range []string{"missing", "other"} {
		code, _ := request(t, http.DefaultClient, "GET", server.URL+"/auth/databases/"+path+"/login-info", nil)
		if code != 404 {
			t.Fatalf("unknown/disabled database exposed: %d", code)
		}
	}
	d, err := f.c.Get("default")
	must(t, err)
	d.Authentication.Access.Mode = "NONE"
	d.Authentication.Access.ProviderID = ""
	must(t, f.c.Save(f.ctx, *d))
	code, _ := request(t, http.DefaultClient, "GET", server.URL+"/auth/databases/default/login-info", nil)
	if code != 400 {
		t.Fatalf("non-account database allowed login: %d", code)
	}
	d, err = f.c.Get("default")
	must(t, err)
	d.MCPEnabled = false
	must(t, f.c.Save(f.ctx, *d))
	code, _ = request(t, http.DefaultClient, "GET", server.URL+"/auth/databases/default/login-info", nil)
	if code != 404 {
		t.Fatalf("disabled database allowed login: %d", code)
	}
}

func TestPublicBusinessTokenLifetimes(t *testing.T) {
	f, server := setupWorkbenchProxy(t, "ACCOUNT")
	v := &oauth.Verifier{Catalog: f.c, Management: f.m, DatabaseID: "default", Issuer: server.URL + "/auth/databases/default", Resource: f.app.MCPBase + "/default"}
	for _, seconds := range []int64{-1, 60, 3600, 86400, 604800, 2592000, 7776000, 31536000, 5400, 0} {
		// -1 represents the legacy request with no lifetime field.
		body := map[string]any{"username": "E1", "password": "business-password"}
		want := seconds
		if seconds == -1 {
			want = 3600
		} else {
			body["expires_in"] = seconds
		}
		before := time.Now().Unix()
		status, raw := request(t, http.DefaultClient, "POST", server.URL+"/auth/databases/default/token", body)
		if status != 200 {
			t.Fatalf("lifetime %d: status %d %s", seconds, status, raw)
		}
		var result struct {
			Token   string `json:"access_token"`
			Expires int64  `json:"expires_in"`
		}
		must(t, json.Unmarshal(raw, &result))
		if result.Expires != want {
			t.Fatalf("lifetime %d returned %d", seconds, result.Expires)
		}
		payload, err := base64.RawURLEncoding.DecodeString(strings.Split(result.Token, ".")[1])
		must(t, err)
		var claims struct {
			Exp       *int64 `json:"exp"`
			Permanent bool   `json:"non_expiring"`
		}
		must(t, json.Unmarshal(payload, &claims))
		info, err := v.Verify(f.ctx, result.Token, nil)
		must(t, err)
		if want == 0 {
			if claims.Exp != nil || !claims.Permanent || !info.Expiration.IsZero() {
				t.Fatal("permanent token has an expiration")
			}
			session := connectMCP(t, f.app.MCPBase+"/default", result.Token)
			out, err := callMCP(t, session, "list_actions", map[string]any{})
			must(t, err)
			if out.IsError {
				t.Fatalf("permanent token could not call MCP: %+v", out)
			}
			session.Close()
		} else if claims.Exp == nil || claims.Permanent || *claims.Exp < before+want || *claims.Exp > time.Now().Unix()+want || info.Expiration.Unix() != *claims.Exp {
			t.Fatalf("incorrect finite expiry: %s", payload)
		}
	}
}

func TestPublicBusinessTokenRejectsInvalidLifetimes(t *testing.T) {
	_, server := setupWorkbenchProxy(t, "ACCOUNT")
	for _, value := range []any{-1, 1, 59, 31536001, 1.5, "forever", 9223372036854775807} {
		status, _ := request(t, http.DefaultClient, "POST", server.URL+"/auth/databases/default/token", map[string]any{"username": "E1", "password": "business-password", "expires_in": value})
		if status != 400 {
			t.Fatalf("invalid lifetime %v: status %d", value, status)
		}
	}
	status, _ := request(t, http.DefaultClient, "POST", server.URL+"/auth/databases/default/token", map[string]any{"username": "E1", "password": "wrong", "expires_in": 0})
	if status != 401 {
		t.Fatalf("permanent token bypassed password: %d", status)
	}
}

func TestPermanentBusinessTokensRemainRevocable(t *testing.T) {
	for _, change := range []string{"password", "disable", "permissions", "authentication"} {
		t.Run(change, func(t *testing.T) {
			f, server := setupWorkbenchProxy(t, "ACCOUNT")
			status, raw := request(t, http.DefaultClient, "POST", server.URL+"/auth/databases/default/token", map[string]any{"username": "E1", "password": "business-password", "expires_in": 0})
			if status != 200 {
				t.Fatalf("issue token: %d %s", status, raw)
			}
			var result struct {
				Token string `json:"access_token"`
			}
			must(t, json.Unmarshal(raw, &result))
			v := &oauth.Verifier{Catalog: f.c, Management: f.m, DatabaseID: "default", Issuer: server.URL + "/auth/databases/default", Resource: f.app.MCPBase + "/default"}
			_, err := v.Verify(f.ctx, result.Token, nil)
			must(t, err)
			if change == "authentication" {
				d, err := f.c.Get("default")
				must(t, err)
				d.Authentication.Access.Mode = "NONE"
				d.Authentication.Access.ProviderID = ""
				must(t, f.c.Save(f.ctx, *d))
			} else {
				a, err := f.m.BusinessAccount(f.ctx, "default", "E1")
				must(t, err)
				password := ""
				switch change {
				case "password":
					password = "reset-password"
				case "disable":
					a.Active = false
				case "permissions":
					a.Permissions = []string{"query"}
				}
				_, err = f.m.SaveBusinessAccount(f.ctx, a, password, a.AuthVersion)
				must(t, err)
			}
			if _, err = v.Verify(f.ctx, result.Token, nil); err == nil {
				t.Fatal("revoked permanent token remained valid")
			}
		})
	}
}

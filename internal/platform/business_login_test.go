package platform_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
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

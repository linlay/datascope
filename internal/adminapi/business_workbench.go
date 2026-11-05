package adminapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"datascope-server/internal/application"
	"datascope-server/internal/guard"
	"datascope-server/internal/management"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// This client uses only a human-supplied business bearer, never platform
// impersonation headers. Tokens live in the request and are never audited.
type businessBearer struct{ token string }

func (b businessBearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	if b.token != "" {
		r.Header.Set("Authorization", "Bearer "+b.token)
	}
	return http.DefaultTransport.RoundTrip(r)
}

func businessWorkbench(app *application.Service, w http.ResponseWriter, r *http.Request, id string) {
	if err := guard.Require(r.Context(), app.Management, id, "action.write"); err != nil {
		if err = guard.Require(r.Context(), app.Management, id, "view.write"); err != nil {
			platformError(w, err)
			return
		}
	}
	d, err := app.Catalog.Get(id)
	if err != nil {
		platformError(w, err)
		return
	}
	p, err := app.Management.ResolveAuth(r.Context(), "DATABASE", id)
	if err != nil {
		platformError(w, err)
		return
	}
	resource := strings.TrimRight(app.MCPBase, "/") + "/" + d.MCPRoute()
	base := strings.TrimSuffix(app.ManagementMCPURL, "/mcp/admin")
	issuer := d.AuthorizationIssuer(base)
	redirect := strings.TrimRight(app.WebURL, "/") + "/api/action-oauth/callback"
	if r.Method == "GET" {
		writeJSON(w, 200, map[string]any{"mode": p.Policy.Mode, "provider_type": p.Provider.Type, "authorization_url": issuer + "/oauth/authorize", "redirect_uri": redirect, "resource": resource})
		return
	}
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var in struct {
		Operation string         `json:"operation"`
		URI       string         `json:"uri"`
		Cursor    string         `json:"cursor"`
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
		Username  string         `json:"username"`
		Password  string         `json:"password"`
		Code      string         `json:"code"`
		ClientID  string         `json:"client_id"`
		Verifier  string         `json:"verifier"`
	}
	if !decode(w, r, &in) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if in.Operation == "call" || in.Operation == "resources/list" || in.Operation == "resources/read" {
		if in.Operation == "call" && !slices.Contains([]string{"list_actions", "describe_action", "invoke_action", "query", "list_tables", "describe_table"}, in.Name) {
			platformError(w, management.ErrDenied)
			return
		}
		if in.Operation == "call" && in.Name == "invoke_action" {
			if err := guard.Require(r.Context(), app.Management, id, "action.write"); err != nil {
				platformError(w, err)
				return
			}
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if p.Policy.Mode != "NONE" && (token == "" || token == r.Header.Get("Authorization")) {
			platformError(w, fmt.Errorf("请先通过业务身份认证"))
			return
		}
		if p.Policy.Mode == "NONE" {
			token = ""
		}
		client := &http.Client{Transport: businessBearer{token}, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		session, e := mcp.NewClient(&mcp.Implementation{Name: "datascope-business-workbench", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: resource, HTTPClient: client, MaxRetries: -1, DisableStandaloneSSE: true}, nil)
		if e != nil {
			platformError(w, fmt.Errorf("业务 MCP 连接失败，请检查认证和服务状态"))
			return
		}
		defer session.Close()
		if in.Operation == "resources/list" {
			out, e := session.ListResources(ctx, &mcp.ListResourcesParams{Cursor: in.Cursor})
			if e != nil {
				platformError(w, fmt.Errorf("无法读取视图列表"))
				return
			}
			writeJSON(w, 200, out)
			return
		}
		if in.Operation == "resources/read" {
			out, e := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: in.URI})
			if e != nil {
				platformError(w, fmt.Errorf("视图不存在或不可访问"))
				return
			}
			writeJSON(w, 200, out)
			return
		}
		out, e := session.CallTool(ctx, &mcp.CallToolParams{Name: in.Name, Arguments: in.Arguments})
		if e != nil {
			platformError(w, fmt.Errorf("业务 MCP 调用失败；写入结果可能未知，请仅查询回执并核对审计"))
			return
		}
		writeJSON(w, 200, out)
		return
	}
	// Pinned existing authentication endpoints, no caller-controlled upstream URL.
	endpoint, contentType := "", "application/json"
	var body []byte
	switch in.Operation {
	case "account_token":
		if p.Policy.Mode != "ACCOUNT" {
			platformError(w, management.ErrDenied)
			return
		}
		endpoint = base + "/auth/databases/" + url.PathEscape(id) + "/token"
		body, _ = json.Marshal(map[string]string{"username": in.Username, "password": in.Password, "resource": resource})
	case "oauth_register":
		if p.Provider.Type != "DEMO_OAUTH" {
			platformError(w, management.ErrDenied)
			return
		}
		endpoint = issuer + "/oauth/register"
		body, _ = json.Marshal(map[string]any{"client_name": "DataScope Action workbench", "redirect_uris": []string{redirect}, "token_endpoint_auth_method": "none"})
	case "oauth_token":
		if p.Provider.Type != "DEMO_OAUTH" {
			platformError(w, management.ErrDenied)
			return
		}
		endpoint = issuer + "/oauth/token"
		contentType = "application/x-www-form-urlencoded"
		body = []byte(url.Values{"grant_type": {"authorization_code"}, "code": {in.Code}, "client_id": {in.ClientID}, "code_verifier": {in.Verifier}, "redirect_uri": {redirect}, "resource": {resource}}.Encode())
	default:
		platformError(w, management.ErrDenied)
		return
	}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		platformError(w, fmt.Errorf("认证端点配置无效"))
		return
	}
	req.Header.Set("Content-Type", contentType)
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		platformError(w, fmt.Errorf("业务认证服务暂不可用"))
		return
	}
	defer res.Body.Close()
	if res.StatusCode != 200 && res.StatusCode != 201 {
		platformError(w, fmt.Errorf("业务认证失败，请检查账号或重新授权"))
		return
	}
	// Only forward documented credentials; upstream error bodies are never logged.
	var result map[string]any
	if err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&result); err != nil {
		platformError(w, fmt.Errorf("业务认证响应无效"))
		return
	}
	out := map[string]any{}
	for _, key := range []string{"client_id", "access_token", "token_type", "expires_in"} {
		if v, ok := result[key]; ok {
			out[key] = v
		}
	}
	writeJSON(w, 200, out)
}

func businessOAuthCallback(app *application.Service, w http.ResponseWriter, r *http.Request) {
	u, err := url.Parse(app.WebURL)
	if err != nil || u.Host == "" {
		http.Error(w, "invalid web URL", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'nonce-business-oauth'; frame-ancestors 'none'; base-uri 'none'")
	code, state, authError := r.URL.Query().Get("code"), r.URL.Query().Get("state"), r.URL.Query().Get("error")
	if code == "" && authError == "" {
		authError = "missing_code"
	}
	businessOAuthPage.Execute(w, map[string]any{"Origin": u.Scheme + "://" + u.Host, "Home": app.WebURL, "Code": code, "State": state, "Error": authError})
}

var businessOAuthPage = template.Must(template.New("businessOAuth").Parse(`<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{if .Error}}未完成授权{{else}}验证完成{{end}} · DataScope</title>
<style>
*{box-sizing:border-box}
body{margin:0;min-height:100vh;min-height:100svh;display:grid;place-items:center;padding:32px 20px;background:#f2f2f7;color:#1d1d1f;font:15px/1.6 -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;-webkit-font-smoothing:antialiased}
main{width:100%;max-width:420px;padding:36px;background:#fff;border:1px solid rgba(60,60,67,.08);border-radius:20px;box-shadow:0 4px 24px rgba(0,0,0,.04)}
h1{margin:0;font-size:24px;font-weight:600}
p{margin:12px 0 28px;color:#63636b}
a,button{display:block;width:100%;padding:13px 16px;border:0;border-radius:10px;background:#0071e3;color:#fff;font:600 15px/1.6 -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;text-align:center;text-decoration:none;cursor:pointer}
button{margin-bottom:12px}a:focus-visible,button:focus-visible{outline:3px solid rgba(0,113,227,.3);outline-offset:3px}
[hidden]{display:none}
@media(max-width:480px){main{padding:28px 24px}}
</style>
</head>
<body>
<main>
<h1>{{if .Error}}未完成授权{{else}}身份验证完成{{end}}</h1>
<p>{{if .Error}}请返回原页面重新连接。{{else}}请返回原页面继续使用。此窗口可以关闭。{{end}}</p>
<button id="return" type="button" hidden>返回原页面</button>
<a href="{{.Home}}">返回 DataScope</a>
</main>
<script nonce="business-oauth">
const result = {type:"datascope-business-oauth",code:{{.Code}},state:{{.State}},error:{{.Error}}};
const origin = {{.Origin}};
const returnButton = document.getElementById("return");
function returnToApp() {
  if (window.opener && !window.opener.closed) {
    try { window.opener.focus(); window.close(); } catch (_) {}
  }
}
if (window.opener && !window.opener.closed) {
  returnButton.hidden = false;
  returnButton.addEventListener("click", returnToApp);
  window.opener.postMessage(result, origin);
  returnToApp();
}
</script>
</body>
</html>`))

package adminapi

import (
	"crypto/subtle"
	"encoding/json"
	"html/template"
	"net/http"
	"time"

	"datascope-server/internal/application"
	"datascope-server/internal/audit"
	"datascope-server/internal/guard"
	"golang.org/x/time/rate"
)

var actionPage = template.Must(template.New("action").Parse(`<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>DataScope · 确认业务操作</title><style>body{font:16px system-ui;max-width:760px;margin:5vh auto;padding:24px;background:#f5f7fa;color:#172b4d}pre{white-space:pre-wrap;overflow-wrap:anywhere;background:white;padding:20px;border-radius:8px}input,button{padding:12px;margin:8px 0;width:100%;box-sizing:border-box}button{background:#165dca;color:white;border:0;border-radius:6px}</style><h1>确认业务操作</h1>{{if .Error}}<p role="alert">{{.Error}}</p>{{end}}{{if .Result}}<pre>{{.Result}}</pre>{{else}}<p>预览已完整执行并回滚。确认后会重新检查权限和业务规则；结果或写入计划发生变化时拒绝提交。确认有效期为 10 分钟。</p><pre>{{.Preview}}</pre><form method="post"><input type="hidden" name="token" value="{{.Token}}"><input type="hidden" name="nonce" value="{{.Nonce}}"><label>当前数据库业务账号<input readonly value="{{.Username}}" autocomplete="username"></label>{{if .Enterprise}}{{if .Verified}}<p>企业身份已认证，点击确认后执行。</p><button type="submit">确认并执行</button>{{else}}<a href="/actions/auth/start?token={{.Token}}">使用企业身份认证</a>{{end}}{{else}}<label>业务账号密码<input required type="password" name="password" autocomplete="current-password"></label><button type="submit">确认并执行</button>{{end}}</form>{{end}}</html>`))

// This route is deliberately outside both MCPs and the platform-session API.
// A model possessing the preview token still cannot execute the operation.
func ActionConfirmationHandler(app *application.Service) http.Handler {
	limiter := rate.NewLimiter(2, 10)
	return http.NewCrossOriginProtection().Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'")
		token := r.URL.Query().Get("token")
		data := map[string]any{}
		ctx := audit.With(r.Context(), audit.Metadata{Source: "action-confirmation", Tool: "confirm_action"})
		if r.Method == http.MethodPost {
			if !limiter.Allow() {
				http.Error(w, "请求过于频繁", 429)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 32768)
			if e := r.ParseForm(); e != nil {
				http.Error(w, "无效表单", 400)
				return
			}
			token = r.PostForm.Get("token")
			cookie, e := r.Cookie("action_confirm_" + tokenPrefix(token))
			nonce := r.PostForm.Get("nonce")
			if e != nil || len(nonce) != 32 || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(nonce)) != 1 {
				http.Error(w, "确认会话失效，请重新打开预览链接", 403)
				return
			}
			out, e := app.ConfirmAction(ctx, token, r.PostForm.Get("password"))
			if e != nil {
				data["Error"] = e.Error()
			} else {
				b, _ := json.MarshalIndent(out, "", "  ")
				data["Result"] = string(b)
			}
		} else if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, "method not allowed", 405)
			return
		}
		if data["Result"] == nil {
			out, username, e := app.ConfirmationInfo(ctx, token)
			if e != nil {
				http.Error(w, "确认已过期、已被处理或不存在", 400)
				return
			}
			b, _ := json.MarshalIndent(out.Preview, "", "  ")
			nonce := audit.ID()
			data["Preview"] = string(b)
			data["Token"] = token
			data["Username"] = username
			rec, err := app.Management.ActionToken(ctx, token)
			if err == nil {
				policy, err := app.Management.ResolveAuth(ctx, "DATABASE", rec.DatabaseID)
				if err == nil && policy.Provider.Type == "ENTERPRISE_OAUTH" {
					data["Enterprise"] = true
					a, ok := guard.Business(r.Context())
					data["Verified"] = ok && a.DatabaseID == rec.DatabaseID && a.Username == username
				}
			}

			data["Nonce"] = nonce
			http.SetCookie(w, &http.Cookie{Name: "action_confirm_" + tokenPrefix(token), Value: nonce, Path: "/actions/confirm", HttpOnly: true, Secure: app.SecureCookies || r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: int((10 * time.Minute).Seconds())})
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		actionPage.Execute(w, data)
	}))
}
func tokenPrefix(s string) string {
	if len(s) > 16 {
		return s[:16]
	}
	return "invalid"
}

package adminapi

import (
	"crypto/sha256"
	"datascope-server/internal/application"
	"datascope-server/internal/audit"
	"datascope-server/internal/guard"
	"datascope-server/internal/management"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type externalFlow struct {
	Policy                              management.ResolvedAuth
	Database, Token, Verifier, Redirect string
	Expires                             time.Time
}
type externalIdentity struct {
	AccessToken string
	Account     management.BusinessAccount
	Fingerprint string
	Expires     time.Time
}
type externalAuth struct {
	app        *application.Service
	mu         sync.Mutex
	flows      map[string]externalFlow
	identities map[string]externalIdentity
}

func newExternalAuth(app *application.Service) *externalAuth {
	return &externalAuth{app: app, flows: map[string]externalFlow{}, identities: map[string]externalIdentity{}}
}
func (s *externalAuth) withIdentity(r *http.Request) *http.Request {
	c, e := r.Cookie("enterprise_action")
	if e != nil {
		return r
	}
	s.mu.Lock()
	identity, ok := s.identities[c.Value]
	s.mu.Unlock()
	if !ok || time.Now().After(identity.Expires) {
		return r
	}
	p, e := s.app.Management.ResolveAuth(r.Context(), "DATABASE", identity.Account.DatabaseID)
	if e != nil || p.Fingerprint() != identity.Fingerprint {
		return r
	}
	a, e := p.VerifyBearer(r.Context(), identity.Account.DatabaseID, identity.AccessToken)
	if e != nil || a.ID != identity.Account.ID {
		return r
	}
	return r.WithContext(guard.WithBusiness(r.Context(), a))
}
func (s *externalAuth) handle(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	if path != "/api/auth/method" && path != "/api/auth/enterprise/start" && path != "/api/auth/enterprise/callback" && path != "/actions/auth/start" {
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "GET" {
		http.Error(w, "method not allowed", 405)
		return true
	}
	m := s.app.Management
	p, e := m.ResolveAuth(r.Context(), "API", "platform")
	if e != nil {
		platformError(w, e)
		return true
	}
	if path == "/api/auth/method" {
		writeJSON(w, 200, map[string]string{"mode": p.Policy.Mode, "name": p.Provider.Name, "type": p.Provider.Type})
		return true
	}
	if path != "/api/auth/enterprise/callback" {
		database, token := "", ""
		if path == "/actions/auth/start" {
			token = r.URL.Query().Get("token")
			rec, err := m.ActionToken(r.Context(), token)
			if err != nil || rec.State != "pending" || rec.Expires <= time.Now().Unix() {
				http.Error(w, "确认已失效", 400)
				return true
			}
			database = rec.DatabaseID
			p, e = m.ResolveAuth(r.Context(), "DATABASE", database)
		}
		if e != nil || p.Provider.Type != "ENTERPRISE_OAUTH" {
			http.Error(w, "未配置企业 OAuth", 400)
			return true
		}
		// The public origin comes from server configuration, never forwarded headers.
		base := strings.TrimSuffix(s.app.ManagementMCPURL, "/mcp/admin")
		if base == "" {
			http.Error(w, "需要配置 ADMIN_PUBLIC_URL", 503)
			return true
		}
		if database == "" && s.app.WebURL != "" {
			base = strings.TrimRight(s.app.WebURL, "/")
		}
		redirect := base + "/api/auth/enterprise/callback"
		state, verifier := audit.ID()+audit.ID(), audit.ID()+audit.ID()
		challenge := sha256.Sum256([]byte(verifier))
		s.mu.Lock()
		for k, v := range s.flows {
			if time.Now().After(v.Expires) {
				delete(s.flows, k)
			}
		}
		for k, v := range s.identities {
			if time.Now().After(v.Expires) {
				delete(s.identities, k)
			}
		}
		if len(s.flows) >= 2048 {
			s.mu.Unlock()
			http.Error(w, "请稍后重试", 429)
			return true
		}
		s.flows[state] = externalFlow{p, database, token, verifier, redirect, time.Now().Add(5 * time.Minute)}
		s.mu.Unlock()
		http.SetCookie(w, &http.Cookie{Name: "enterprise_state_" + state[:16], Value: state, Path: "/api/auth/enterprise/callback", HttpOnly: true, Secure: s.app.SecureCookies || r.TLS != nil, SameSite: http.SameSiteLaxMode, MaxAge: 300})
		u, _ := url.Parse(p.Provider.AuthorizeURL)
		q := u.Query()
		for k, v := range map[string]string{"response_type": "code", "client_id": p.Provider.ClientID, "redirect_uri": redirect, "state": state, "scope": "openid profile", "code_challenge": base64.RawURLEncoding.EncodeToString(challenge[:]), "code_challenge_method": "S256"} {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
		http.Redirect(w, r, u.String(), 303)
		return true
	}
	state := r.URL.Query().Get("state")
	if len(state) != 64 {
		http.Error(w, "认证会话失效", 403)
		return true
	}
	cookie, err := r.Cookie("enterprise_state_" + state[:16])
	if err != nil || cookie.Value != state {
		http.Error(w, "认证会话失效", 403)
		return true
	}
	s.mu.Lock()
	flow, ok := s.flows[state]
	delete(s.flows, state)
	s.mu.Unlock()
	kind, id := "API", "platform"
	if flow.Database != "" {
		kind, id = "DATABASE", flow.Database
	}
	current, err := m.ResolveAuth(r.Context(), kind, id)
	if !ok || time.Now().After(flow.Expires) || err != nil || current.Fingerprint() != flow.Policy.Fingerprint() {
		http.Error(w, "认证配置或会话已变化", 403)
		return true
	}
	if iss := r.URL.Query().Get("iss"); iss != "" && iss != current.Provider.Issuer {
		http.Error(w, "认证来源不匹配", 403)
		return true
	}
	a, accessToken, err := current.ExchangeCode(r.Context(), flow.Database, r.URL.Query().Get("code"), flow.Verifier, flow.Redirect)
	if err != nil {
		http.Error(w, "企业认证失败", 401)
		return true
	}
	if flow.Database == "" {
		u, err := m.PlatformIdentity(r.Context(), current, a)
		if err != nil {
			http.Error(w, "企业身份尚未获得平台访问权限", 403)
			return true
		}
		token, err := m.NewSession(r.Context(), u, "web", 12*time.Hour)
		if err != nil {
			platformError(w, err)
			return true
		}
		http.SetCookie(w, &http.Cookie{Name: "platform_session", Value: token, Path: "/", HttpOnly: true, Secure: s.app.SecureCookies || r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: 43200})
		returnTo := s.app.WebURL
		if returnTo == "" {
			returnTo = "/"
		}
		http.Redirect(w, r, returnTo, 303)
	} else {
		token := audit.ID() + audit.ID()
		s.mu.Lock()
		s.identities[token] = externalIdentity{accessToken, a, current.Fingerprint(), time.Now().Add(10 * time.Minute)}
		s.mu.Unlock()
		http.SetCookie(w, &http.Cookie{Name: "enterprise_action", Value: token, Path: "/actions/", HttpOnly: true, Secure: s.app.SecureCookies || r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: 600})
		http.Redirect(w, r, "/actions/confirm?token="+url.QueryEscape(flow.Token), 303)
	}
	return true
}

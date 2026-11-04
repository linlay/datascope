package oauth

import (
	"context"
	"datascope-server/internal/audit"
	"datascope-server/internal/catalog"
	"datascope-server/internal/management"
	"datascope-server/internal/store"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func (s *Server) realm(database string) (*Server, error) {
	if s.cfg.Catalog == nil {
		return nil, fmt.Errorf("未配置业务认证来源")
	}
	d, e := s.cfg.Catalog.Get(database)
	if e != nil || !d.MCPEnabled || !d.LocalAuthentication() {
		return nil, fmt.Errorf("业务认证来源未开启")
	}
	p, e := s.cfg.Management.ResolveAuth(context.Background(), "DATABASE", d.ID)
	if e != nil || p.Provider.Type != "DEMO_OAUTH" {
		return nil, management.ErrDenied
	}
	scoped := *s
	scoped.cfg = s.cfg
	scoped.cfg.AccountDatabase = d.ID
	scoped.cfg.Issuer = d.AuthorizationIssuer(s.cfg.Issuer)
	return &scoped, nil
}
func (s *Server) realmHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/auth/databases/"+r.PathValue("database")+"/oauth/jwks.json" && r.Method == "GET" {
		d, err := s.cfg.Catalog.Get(r.PathValue("database"))
		if err == nil && d.MCPEnabled {
			jsonOut(w, 200, s.JWKS())
			return
		}
	}
	scoped, e := s.realm(r.PathValue("database"))
	if e != nil {
		http.Error(w, e.Error(), 404)
		return
	}
	prefix := "/auth/databases/" + r.PathValue("database")
	http.StripPrefix(prefix, scoped.Handler(http.NotFoundHandler())).ServeHTTP(w, r)
}
func (s *Server) realmMetadata(w http.ResponseWriter, r *http.Request) {
	scoped, e := s.realm(r.PathValue("database"))
	if e != nil {
		http.NotFound(w, r)
		return
	}
	scoped.metadata(w, r)
}
func (s *Server) localDatabase() (*catalog.Database, error) {
	if s.cfg.Catalog == nil || s.cfg.AccountDatabase == "" {
		return nil, fmt.Errorf("未配置业务认证来源")
	}
	return s.cfg.Catalog.Get(s.cfg.AccountDatabase)
}
func (s *Server) businessEmployee(ctx context.Context, username string) (store.UserSummary, error) {
	d, e := s.localDatabase()
	if e != nil {
		return store.UserSummary{}, e
	}
	a, e := s.cfg.Catalog.BusinessAccount(ctx, d, username)
	if e != nil {
		return store.UserSummary{}, e
	}
	return store.UserSummary{ID: a.ID, EmpNo: a.Username, Name: a.Name, Status: "active", AuthVersion: a.AuthVersion}, nil
}
func (s *Server) authenticateBusiness(ctx context.Context, username, password string) (store.UserSummary, error) {
	a, e := s.cfg.Management.AuthenticateBusiness(ctx, s.cfg.AccountDatabase, username, password)
	if e != nil {
		return store.UserSummary{}, e
	}
	u, e := s.businessEmployee(ctx, a.Username)
	if e != nil || u.AuthVersion != a.AuthVersion {
		return store.UserSummary{}, management.ErrDenied
	}
	return u, nil
}
func (s *Server) identity() string {
	if s.cfg.AccountDatabase != "" {
		return "database:" + s.cfg.AccountDatabase
	}
	if s.cfg.Management != nil {
		return "platform"
	}
	return "employee"
}
func (s *Server) authConfig() string {
	if s.cfg.AccountDatabase == "" && s.cfg.Management != nil {
		p, e := s.cfg.Management.ResolveAuth(context.Background(), "API", "platform")
		if e != nil {
			return "invalid"
		}
		return p.Fingerprint()
	}
	d, e := s.localDatabase()
	if e != nil {
		return ""
	}
	return d.AuthenticationVersion()
}
func (s *Server) loginPath() string {
	if s.cfg.AccountDatabase != "" {
		return "/auth/databases/" + s.cfg.AccountDatabase + "/oauth/authorize"
	}
	return "/oauth/authorize"
}
func (v *Verifier) businessToken(ctx context.Context, sub, identity, config string, version int64, scopes []string) (management.BusinessAccount, error) {
	if v.Catalog == nil {
		return management.BusinessAccount{}, management.ErrDenied
	}
	d, e := v.Catalog.Get(v.DatabaseID)
	if e != nil || !d.MCPEnabled || !d.LocalAuthentication() || identity != "database:"+d.ID || config != d.AuthenticationVersion() {
		return management.BusinessAccount{}, management.ErrDenied
	}
	p, e := v.Catalog.Management.ResolveAuth(ctx, "DATABASE", d.ID)
	if e != nil || p.Provider.Type != "DEMO_OAUTH" {
		return management.BusinessAccount{}, management.ErrDenied
	}
	a, e := v.Catalog.BusinessAccount(ctx, d, sub)
	if e != nil || a.AuthVersion != version {
		return management.BusinessAccount{}, management.ErrDenied
	}
	for _, scope := range scopes {
		if scope != "sql:query" {
			return management.BusinessAccount{}, management.ErrDenied
		}
	}
	return a, nil
}
func (s *Server) databaseForResource(resource string) string {
	if s.cfg.DatabaseForResource == nil {
		return ""
	}
	database, _ := s.cfg.DatabaseForResource(resource)
	return database
}
func (s *Server) redirectRealm(w http.ResponseWriter, r *http.Request) bool {
	if s.cfg.Catalog == nil || s.cfg.AccountDatabase != "" {
		return false
	}
	d, e := s.cfg.Catalog.Get(s.databaseForResource(r.URL.Query().Get("resource")))
	if e != nil || !d.LocalAuthentication() {
		return false
	}
	http.Redirect(w, r, d.AuthorizationIssuer(s.cfg.Issuer)+"/oauth/authorize?"+r.URL.RawQuery, http.StatusSeeOther)
	return true
}

func (s *Server) authConfigMatches(g grant) bool {
	return g.Realm == s.cfg.AccountDatabase && g.AuthConfig == s.authConfig()
}
func databaseIdentity(identity string) bool {
	return strings.HasPrefix(identity, "database:")
}

// accountToken exchanges credentials for a database-bound bearer token. It is
// independent of the OAuth authorization-code endpoints and stores no session.
func (s *Server) accountToken(w http.ResponseWriter, r *http.Request) {
	if !s.loginLimit.Allow() {
		http.Error(w, "登录过于频繁", 429)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Resource string `json:"resource"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		jsonOut(w, 400, map[string]string{"error": "无效请求"})
		return
	}
	d, err := s.cfg.Catalog.Get(r.PathValue("database"))
	if err != nil || !d.MCPEnabled {
		http.NotFound(w, r)
		return
	}
	p, err := s.cfg.Management.ResolveAuth(r.Context(), "DATABASE", d.ID)
	if err != nil || p.Policy.Mode != "ACCOUNT" {
		jsonOut(w, 400, map[string]string{"error": "当前数据库未启用账号密码认证"})
		return
	}
	resource := in.Resource
	if resource == "" {
		resource = strings.TrimRight(s.cfg.Resource, "/") + "/" + d.MCPRoute()
	}
	if !s.allowsResource(resource) || s.databaseForResource(resource) != d.ID {
		jsonOut(w, 400, map[string]string{"error": "无效数据库资源"})
		return
	}
	auditor := s.cfg.Management.AuditFor(d.ID)
	ctx := audit.With(r.Context(), audit.Metadata{DatabaseID: d.ID, Source: "business-login", Tool: "account_token"})
	ev, _ := auditor.Start(ctx, "auth.account.login", "")
	result := error(management.ErrDenied)
	defer func() { _ = auditor.Finish(ev, result) }()
	a, err := s.cfg.Management.VerifyAccount(r.Context(), p, d.ID, in.Username, in.Password)
	if err == nil && p.Local() {
		a, err = s.cfg.Catalog.BusinessAccount(r.Context(), d, in.Username)
	}
	if err != nil {
		jsonOut(w, 401, map[string]string{"error": "账号或密码错误"})
		return
	}
	token, err := s.signClaims(map[string]any{"iss": d.AuthorizationIssuer(s.cfg.Issuer), "aud": resource, "sub": a.Username, "iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(), "scope": "sql:query", "jti": random(), "identity": "database:" + d.ID, "auth_config": d.AuthenticationVersion(), "business": a})
	if err != nil {
		http.Error(w, "signing failed", 500)
		return
	}
	result = nil
	if ev != nil {
		ev.SubjectID = a.BusinessUserID
	}
	jsonOut(w, 200, map[string]any{"access_token": token, "token_type": "Bearer", "expires_in": 3600})
}
func (v *Verifier) accountIdentity(ctx context.Context, a management.BusinessAccount, identity, config string) (management.BusinessAccount, error) {
	if v.Catalog == nil || v.Catalog.Management == nil {
		return a, management.ErrDenied
	}
	d, err := v.Catalog.Get(v.DatabaseID)
	if err != nil || !d.MCPEnabled || identity != "database:"+d.ID || config != d.AuthenticationVersion() || a.DatabaseID != d.ID || !a.Active {
		return a, management.ErrDenied
	}
	p, err := v.Catalog.Management.ResolveAuth(ctx, "DATABASE", d.ID)
	if err != nil || p.Policy.Mode != "ACCOUNT" {
		return a, management.ErrDenied
	}
	if p.Local() {
		current, err := v.Catalog.BusinessAccount(ctx, d, a.Username)
		if err != nil || current.ID != a.ID || current.AuthVersion != a.AuthVersion {
			return a, management.ErrDenied
		}
		return current, nil
	}
	return a, nil
}

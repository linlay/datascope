package oauth

import (
	"context"
	"datascope-server/internal/catalog"
	"datascope-server/internal/management"
	"datascope-server/internal/store"
	"fmt"
	"net/http"
	"strings"
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

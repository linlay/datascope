package mcpserver

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"datascope-server/internal/catalog"
	"datascope-server/internal/guard"
	"datascope-server/internal/management"
	"datascope-server/internal/oauth"
)

func policyAuthentication(next http.Handler, c *catalog.Catalog, d *catalog.Database, p management.ResolvedAuth, resource, issuer, path string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/.well-known/oauth-protected-resource") {
			w.Header().Set("Content-Type", "application/json")
			metadata := map[string]any{"resource": resource}
			if p.Policy.Mode == "OAUTH" {
				metadata["authorization_servers"] = []string{p.Provider.Issuer}
				metadata["bearer_methods_supported"] = []string{"header"}
			}
			json.NewEncoder(w).Encode(metadata)
			return
		}
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		var a management.BusinessAccount
		var e error
		switch p.Policy.Mode {
		case "NONE":
			a = management.BusinessAccount{DatabaseID: d.ID, Username: "anonymous", BusinessUserID: p.Policy.BusinessUserID, Permissions: p.Policy.Permissions, Active: true, AuthVersion: int64(p.Revision)}
		case "ACCOUNT":
			parts := strings.Fields(r.Header.Get("Authorization"))
			if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
				v := &oauth.Verifier{Catalog: c, Management: c.Management, DatabaseID: d.ID, Issuer: d.AuthorizationIssuer(issuer), Resource: resource}
				info, err := v.Verify(r.Context(), parts[1], r)
				e = err
				if err == nil {
					var ok bool
					a, ok = info.Extra["business_account"].(management.BusinessAccount)
					if !ok {
						e = management.ErrDenied
					}
				}
				break
			}
			username, password, ok := r.BasicAuth()
			if !ok {
				w.Header().Set("WWW-Authenticate", `Basic realm="DataScope"`)
				http.Error(w, "需要业务账号认证", 401)
				return
			}
			a, e = c.Management.VerifyAccount(r.Context(), p, d.ID, username, password)
			if e == nil && p.Provider.Type == "BUSINESS_ACCOUNT" {
				a, e = c.BusinessAccount(r.Context(), d, username)
			}
		case "OAUTH":
			token := ""
			parts := strings.Fields(r.Header.Get("Authorization"))
			if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
				token = parts[1]
			}
			a, e = p.VerifyBearer(r.Context(), d.ID, token)
		default:
			e = management.ErrDenied
		}
		if e != nil {
			if p.Policy.Mode == "OAUTH" {
				u, _ := url.Parse(resource)
				w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+u.Scheme+"://"+u.Host+"/.well-known/oauth-protected-resource"+u.Path+`"`)
			}
			http.Error(w, "身份认证失败", 401)
			return
		}
		next.ServeHTTP(w, r.WithContext(guard.WithBusiness(r.Context(), a)))
	})
}

package mcpserver

import (
	"context"
	"datascope-server/internal/catalog"
	"datascope-server/internal/management"
	"datascope-server/internal/oauth"
	"datascope-server/internal/store"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// HTTPHandler serves the same protection for initialization, tools and every HTTP method.
func HTTPHandler(server *mcp.Server, st *store.Store, mode, issuer, resource, path string, stateless bool) (http.Handler, error) {
	return HTTPHandlerWithManagement(server, st, mode, issuer, resource, path, stateless, nil, "")
}
func httpHandlerWithManagement(server *mcp.Server, st *store.Store, mode, issuer, resource, path string, stateless bool, manager *management.Store, database string, catalogues ...*catalog.Catalog) (http.Handler, error) {
	if mode != "oauth" && mode != "header" && mode != "none" {
		return nil, fmt.Errorf("unknown MCP_AUTH_MODE %q", mode)
	}
	h := http.Handler(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: stateless}))
	mux := http.NewServeMux()
	if mode == "oauth" {
		issuer = strings.TrimRight(issuer, "/")
		if !oauth.ValidURL(issuer, false) || !oauth.ValidURL(resource, false) {
			return nil, fmt.Errorf("invalid OAuth issuer/resource URL")
		}
		u, _ := url.Parse(resource)
		base := u.Scheme + "://" + u.Host
		metaPath := "/.well-known/oauth-protected-resource" + strings.TrimRight(u.Path, "/")
		scopes := []string{"sql:query", "sql:debug"}
		if len(catalogues) > 0 && catalogues[0] != nil && catalogues[0].Management != nil {
			if d, e := catalogues[0].Get(database); e == nil && d.LocalAuthentication() {
				scopes = []string{"sql:query"}
			}
		}
		metadata := auth.ProtectedResourceMetadataHandler(&oauthex.ProtectedResourceMetadata{Resource: resource, AuthorizationServers: []string{issuer}, ScopesSupported: scopes, BearerMethodsSupported: []string{"header"}})
		mux.Handle(metaPath, metadata)
		if metaPath != "/.well-known/oauth-protected-resource" {
			mux.Handle("/.well-known/oauth-protected-resource", metadata)
		}
		var catalogue *catalog.Catalog
		if len(catalogues) > 0 {
			catalogue = catalogues[0]
		}
		verifier := &oauth.Verifier{Catalog: catalogue, Issuer: issuer, Resource: resource, Store: st, Management: manager, DatabaseID: database}
		h = auth.RequireBearerToken(verifier.Verify, &auth.RequireBearerTokenOptions{ResourceMetadataURL: base + metaPath, Scopes: []string{"sql:query"}})(h)
	}
	mux.Handle(path, h)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})
	return mux, nil
}

func HTTPHandlerWithManagement(server *mcp.Server, st *store.Store, mode, issuer, resource, path string, stateless bool, manager *management.Store, database string, catalogues ...*catalog.Catalog) (http.Handler, error) {
	base, e := httpHandlerWithManagement(server, st, mode, issuer, resource, path, stateless, manager, database, catalogues...)
	if e != nil || manager == nil || database != "" {
		return base, e
	}
	unsigned, e := httpHandlerWithManagement(server, st, "none", issuer, resource, path, stateless, manager, database, catalogues...)
	if e != nil {
		return nil, e
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, e := manager.ResolveAuth(r.Context(), "API", "management-mcp")
		if e != nil {
			http.Error(w, "认证配置不可用", 503)
			return
		}
		if p.Provider.Type != "ENTERPRISE_OAUTH" {
			base.ServeHTTP(w, r)
			return
		}
		u, _ := url.Parse(resource)
		metaPath := "/.well-known/oauth-protected-resource" + strings.TrimRight(u.Path, "/")
		if r.URL.Path == metaPath || r.URL.Path == "/.well-known/oauth-protected-resource" {
			auth.ProtectedResourceMetadataHandler(&oauthex.ProtectedResourceMetadata{Resource: resource, AuthorizationServers: []string{p.Provider.Issuer}, BearerMethodsSupported: []string{"header"}}).ServeHTTP(w, r)
			return
		}
		// A fresh UserInfo check is required for each HTTP request. This short
		// lease is proof freshness for SDK middleware, never a cached token lifetime.
		verify := func(ctx context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
			a, e := p.VerifyBearer(ctx, "", token)
			if e != nil {
				return nil, auth.ErrInvalidToken
			}
			u, e := manager.PlatformIdentity(ctx, p, a)
			if e != nil {
				return nil, auth.ErrInvalidToken
			}
			return &auth.TokenInfo{UserID: strconv.FormatInt(u.ID, 10), Expiration: time.Now().Add(10 * time.Second), Scopes: []string{"sql:query"}}, nil
		}
		auth.RequireBearerToken(verify, &auth.RequireBearerTokenOptions{ResourceMetadataURL: u.Scheme + "://" + u.Host + metaPath})(unsigned).ServeHTTP(w, r)
	}), nil
}

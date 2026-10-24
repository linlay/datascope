package mcpserver

import (
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
	"strings"
)

// HTTPHandler serves the same protection for initialization, tools and every HTTP method.
func HTTPHandler(server *mcp.Server, st *store.Store, mode, issuer, resource, path string, stateless bool) (http.Handler, error) {
	return HTTPHandlerWithManagement(server, st, mode, issuer, resource, path, stateless, nil, "")
}
func HTTPHandlerWithManagement(server *mcp.Server, st *store.Store, mode, issuer, resource, path string, stateless bool, manager *management.Store, database string, catalogues ...*catalog.Catalog) (http.Handler, error) {
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
		if len(catalogues) > 0 && catalogues[0] != nil {
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

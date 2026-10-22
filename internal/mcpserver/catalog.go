package mcpserver

import (
	"context"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"slices"
	"sql-rewrite-server/internal/catalog"
	"sql-rewrite-server/internal/service"
	"sql-rewrite-server/internal/store"
	"strings"
)

// ManagedHTTPHandler resolves current configuration on every request. Stateless
// sessions ensure a previously connected client cannot retain revoked tools.
// Identity verification uses the original identity database for all data sources.
func ManagedHTTPHandler(c *catalog.Catalog, identity *store.Store, opts Options, mode, issuer, resource, path string) (http.Handler, error) {
	// Validate authentication configuration at startup using the existing handler.
	if _, err := HTTPHandler(nil, identity, mode, issuer, resource, path, true); err != nil {
		return nil, err
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"status":"ok"}`))
			return
		}
		segment := ""
		prefix := strings.TrimRight(path, "/") + "/"
		metaPrefix := "/.well-known/oauth-protected-resource" + prefix
		switch {
		case r.URL.Path == path:
		case strings.HasPrefix(r.URL.Path, prefix):
			segment = strings.TrimPrefix(r.URL.Path, prefix)
		case strings.HasPrefix(r.URL.Path, metaPrefix):
			segment = strings.TrimPrefix(r.URL.Path, metaPrefix)
		case r.URL.Path == "/.well-known/oauth-protected-resource" || r.URL.Path == "/.well-known/oauth-protected-resource"+path:
		default:
			http.NotFound(w, r)
			return
		}
		var d *catalog.Database
		var err error
		if segment == "" {
			d, err = c.Get("default")
		} else {
			d, err = c.ByMCPPath(segment)
		}
		if err != nil {
			http.Error(w, "数据库不存在", 404)
			return
		}
		if !d.MCPEnabled {
			http.Error(w, "该数据库未开启 MCP", 403)
			return
		}
		svc, err := c.Open(r.Context(), d)
		if err != nil {
			http.Error(w, err.Error(), 503)
			return
		}
		defer svc.Store.Close()
		selected := opts
		selected.IdentityStore = identity
		selected.Tools = d.Tools
		selected.Managed = true
		endpoint := path
		if segment != "" {
			endpoint = prefix + segment
		}
		// Discovery, authorization resource and token audience identify the exact
		// endpoint. Build from the pinned public URL, never the request Host.
		endpointResource := resource
		if segment != "" {
			endpointResource = strings.TrimRight(resource, "/") + "/" + segment
		}
		h, err := HTTPHandler(New(svc, selected), identity, mode, issuer, endpointResource, endpoint, true)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		h.ServeHTTP(w, r)
	}), nil
}

// ManagedServer applies the same live catalog rules to long-lived transports
// such as stdio. Request-local services avoid races between concurrent calls.
func ManagedServer(c *catalog.Catalog, id string, svc *service.QueryService, opts Options) *mcp.Server {
	opts.Managed = true
	opts.Tools = catalog.Tools
	server := New(svc, opts)
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			d, err := c.Get(id)
			if err != nil {
				return nil, err
			}
			if !d.MCPEnabled {
				return nil, fmt.Errorf("该数据库未开启 MCP")
			}
			switch method {
			case "tools/list":
				res, err := next(ctx, method, req)
				if err != nil {
					return nil, err
				}
				list := res.(*mcp.ListToolsResult)
				allowed := []*mcp.Tool{}
				for _, tool := range list.Tools {
					if slices.Contains(d.Tools, tool.Name) {
						allowed = append(allowed, tool)
					}
				}
				list.Tools = allowed
				return list, nil
			case "tools/call":
				call := req.(*mcp.CallToolRequest)
				if !slices.Contains(d.Tools, call.Params.Name) {
					return nil, fmt.Errorf("该数据库未开启接口 %s", call.Params.Name)
				}
				current, err := c.Open(ctx, d)
				if err != nil {
					return nil, err
				}
				defer current.Store.Close()
				ctx = context.WithValue(ctx, serviceContextKey{}, current)
			}
			return next(ctx, method, req)
		}
	})
	return server
}

type serviceContextKey struct{}

func (h handlers) current(ctx context.Context) handlers {
	if svc, ok := ctx.Value(serviceContextKey{}).(*service.QueryService); ok {
		h.svc = svc
	}
	return h
}

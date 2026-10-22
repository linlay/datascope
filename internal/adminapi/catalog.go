package adminapi

import (
	"context"
	"net/http"
	"sql-rewrite-server/internal/catalog"
	"strings"
)

type databaseView struct {
	catalog.Database
	MCPEndpoint string `json:"mcp_endpoint"`
}

// ManagedHandler scopes every existing console API to an explicit database.
func ManagedHandler(c *catalog.Catalog, inspector *Inspector) http.Handler {
	view := func(d catalog.Database) databaseView {
		return databaseView{Database: d, MCPEndpoint: strings.TrimRight(inspector.Endpoint, "/") + "/" + d.MCPRoute()}
	}
	respondDatabase := func(w http.ResponseWriter, d *catalog.Database, err error) {
		if err != nil {
			respond(w, nil, err)
			return
		}
		respond(w, view(*d), nil)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/databases", func(w http.ResponseWriter, r *http.Request) {
		items, err := c.List()
		if err != nil {
			respond(w, nil, err)
			return
		}
		// Connection details are available only through the local admin API.
		views := make([]databaseView, len(items))
		for i, d := range items {
			views[i] = view(d)
		}
		respond(w, views, nil)
	})
	mux.HandleFunc("POST /api/databases", func(w http.ResponseWriter, r *http.Request) {
		var d catalog.Database
		if !decode(w, r, &d) {
			return
		}
		if err := c.Add(r.Context(), d); err != nil {
			respond(w, nil, err)
			return
		}
		item, err := c.Get(d.ID)
		respondDatabase(w, item, err)
	})
	mux.HandleFunc("GET /api/databases/{id}", func(w http.ResponseWriter, r *http.Request) {
		d, err := c.Get(r.PathValue("id"))
		respondDatabase(w, d, err)
	})
	mux.HandleFunc("PUT /api/databases/{id}", func(w http.ResponseWriter, r *http.Request) {
		var d catalog.Database
		if !decode(w, r, &d) {
			return
		}
		d.ID = r.PathValue("id")
		if err := c.Save(r.Context(), d); err != nil {
			respond(w, nil, err)
			return
		}
		item, err := c.Get(d.ID)
		respondDatabase(w, item, err)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		id := "default"
		path := r.URL.Path
		if strings.HasPrefix(path, "/api/databases/") {
			parts := strings.SplitN(strings.TrimPrefix(path, "/api/databases/"), "/", 2)
			if len(parts) != 2 {
				http.NotFound(w, r)
				return
			}
			id = parts[0]
			path = "/api/" + parts[1]
		}
		if path == "/api/employees" || path == "/api/users" {
			users, err := inspector.Service.Store.Users(r.Context())
			respond(w, users, err)
			return
		}
		d, err := c.Get(id)
		if err != nil {
			writeJSON(w, 404, map[string]string{"error": err.Error()})
			return
		}
		svc, err := c.Open(r.Context(), d)
		if err != nil {
			respond(w, nil, err)
			return
		}
		defer svc.Store.Close()
		in := *inspector
		in.Service = svc
		if id != "default" {
			in.FixturesDir = ""
		}
		in.Endpoint = view(*d).MCPEndpoint
		if inspector.TokenForDatabase != nil {
			in.Token = func(ctx context.Context, userID int64) (string, error) {
				return inspector.TokenForDatabase(ctx, userID, d.ID)
			}
		}
		clone := r.Clone(r.Context())
		u := *r.URL
		u.Path = path
		clone.URL = &u
		Handler(svc, &in).ServeHTTP(w, clone)
	})
	return localhostOnlyHost(http.NewCrossOriginProtection().Handler(mux))
}

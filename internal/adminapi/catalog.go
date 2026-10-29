package adminapi

import (
	"context"
	"datascope-server/internal/catalog"
	"datascope-server/internal/guard"
	"datascope-server/internal/store"
	"net/http"
	"strconv"
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
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
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

		d, err := c.Get(id)
		if err != nil {
			writeJSON(w, 404, map[string]string{"error": err.Error()})
			return
		}
		if c.Management != nil && strings.HasPrefix(path, "/api/mcp/") {
			writeJSON(w, 400, map[string]string{"error": "该数据库使用独立业务认证，请通过业务连接器使用员工工号和密码授权后测试"})
			return
		}
		// Inspection and browsing must survive schema drift and broken custom
		// policies. PlatformHandler has already checked config.read/raw_query.
		if r.Method == "GET" && (path == "/api/tables" || strings.HasPrefix(path, "/api/tables/")) {
			st, err := c.OpenStore(r.Context(), d)
			if err != nil {
				respond(w, nil, err)
				return
			}
			defer st.Close()
			if path == "/api/tables" {
				tables, err := st.Tables(r.Context())
				out := make([]tableInfo, len(tables))
				for i, t := range tables {
					out[i].Table = t
					if d.Config != nil {
						for _, p := range d.Config.Tables {
							if strings.EqualFold(p.Table, t.Name) {
								out[i].Scheme = p.Scheme
							}
						}
					}
				}
				respond(w, out, err)
				return
			}
			name := strings.TrimPrefix(path, "/api/tables/")
			if strings.HasSuffix(name, "/rows") {
				q := r.URL.Query()
				limit, _ := strconv.Atoi(q.Get("limit"))
				offset, _ := strconv.Atoi(q.Get("offset"))
				out, err := st.Browse(r.Context(), store.BrowseOptions{Table: strings.TrimSuffix(name, "/rows"), Limit: limit, Offset: offset, OrderBy: q.Get("order"), Desc: q.Get("desc") == "1", Search: q.Get("q"), FilterColumn: q.Get("col"), FilterValue: q.Get("val")})
				respond(w, out, err)
				return
			}
			out, err := st.Describe(r.Context(), name)
			respond(w, out, err)
			return
		}
		svc, err := c.Open(r.Context(), d)
		if err != nil {
			respond(w, nil, err)
			return
		}
		defer svc.Store.Close()
		in := *inspector
		if u, ok := guard.User(r.Context()); ok {
			in.CurrentUserID = u.ID
		}
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

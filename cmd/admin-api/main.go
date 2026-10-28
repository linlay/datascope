// Command admin-api is the backend for the super-admin console: it serves the
// demo database to the frontend and inspects the MCP server.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"datascope-server/internal/adminapi"
	"datascope-server/internal/application"
	"datascope-server/internal/audit"
	"datascope-server/internal/catalog"
	"datascope-server/internal/envflag"
	"datascope-server/internal/managementmcp"
	"datascope-server/internal/mcpserver"
	"datascope-server/internal/oauth"
	"datascope-server/internal/platform"
	"datascope-server/internal/store"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server exited", "error", err)
		os.Exit(1)
	}
}

func run() error {
	dbPath := flag.String("db", envflag.StringAllowEmpty("SQLITE_PATH", "data/securities_demo.sqlite3"), "可选初始 SQLite 路径；空值禁用演示库导入")
	addr := flag.String("addr", envflag.String("ADMIN_API_ADDR", "127.0.0.1:8081"), "HTTP 监听地址")
	mcpURL := flag.String("mcp-url", envflag.String("MCP_SERVER_URL", "http://127.0.0.1:8080/mcp"), "被检查的 MCP server 端点")
	fixtures := flag.String("fixtures-dir", envflag.String("FIXTURES_DIR", "fixtures"), "权限与改写验收案例目录，用于校验 MCP")
	userHeader := flag.String("mcp-user-header", envflag.String("MCP_USER_HEADER", mcpserver.DefaultUserHeader), "MCP server 信任的用户身份请求头")
	queryTimeout := flag.Duration("query-timeout", envflag.Duration("QUERY_TIMEOUT", store.DefaultQueryTimeout), "单条查询超时")
	rewriteConfig := flag.String("rewrite-config", envflag.String("REWRITE_CONFIG", "config/rewrite.yml"), "SQL 改写规则配置文件")
	asOf := flag.String("as-of", envflag.String("REWRITE_AS_OF", store.DefaultAsOf.Format(time.RFC3339)), "授权过期判断的参考时间（RFC 3339），需与 MCP server 一致")
	flag.Parse()
	asOfTime, err := time.Parse(time.RFC3339, *asOf)
	if err != nil {
		return fmt.Errorf("-as-of: %w", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	var st *store.Store
	if *dbPath != "" {
		st, err = store.Open(*dbPath, *queryTimeout)
		if err != nil {
			return err
		}
		defer st.Close()
	}
	manager, err := platform.Open(context.Background())
	if err != nil {
		return err
	}
	defer manager.Close()
	defer manager.Audit.Close()
	catalogue, err := catalog.BootstrapManaged(context.Background(), manager, envflag.String("DATABASE_CONFIG_DIR", "config/databases"), st, *rewriteConfig, asOfTime, *queryTimeout, envflag.String("DATABASE_CATALOG", "data/databases.json"))
	if err != nil {
		return err
	}
	if err = platform.InitializeDemoRealm(context.Background(), catalogue, envflag.String("OAUTH_DEMO_PASSWORD", "")); err != nil {
		return err
	}

	inspector := &adminapi.Inspector{
		Endpoint:    *mcpURL,
		Service:     nil,
		FixturesDir: *fixtures,
		UserHeader:  *userHeader,
		HTTPClient:  &http.Client{Timeout: 60 * time.Second},
	}

	adminResource := strings.TrimRight(envflag.String("OAUTH_ISSUER", "http://127.0.0.1:8081"), "/") + "/mcp/admin"
	app := &application.Service{ManagementMCPURL: adminResource, Management: manager, Catalog: catalogue, MCPBase: envflag.String("MCP_RESOURCE_URL", "http://127.0.0.1:8080/mcp"), SecureCookies: envflag.Bool("SESSION_COOKIE_SECURE", strings.HasPrefix(envflag.String("OAUTH_ISSUER", "http://127.0.0.1:8081"), "https://"))}
	handler := adminapi.PlatformHandler(app, adminapi.ManagedHandler(catalogue, inspector))
	mode := envflag.String("MCP_AUTH_MODE", "oauth")
	inspector.AuthMode = mode
	if mode != "oauth" && mode != "header" && mode != "none" {
		return fmt.Errorf("invalid MCP_AUTH_MODE %q", mode)
	}
	if mode == "oauth" {
		resourceBase := envflag.String("MCP_RESOURCE_URL", *mcpURL)
		as, err := oauth.New(oauth.Config{
			Catalog:    catalogue,
			Issuer:     envflag.String("OAUTH_ISSUER", "http://127.0.0.1:8081"),
			Resource:   resourceBase,
			Management: manager,
			ResourceAllowed: func(resource string) bool {
				return resource == adminResource || catalogue.AllowsMCPResource(resourceBase, resource)
			},
			DatabaseForResource: func(resource string) (string, error) {
				if resource == adminResource {
					return "", nil
				}
				items, e := catalogue.List()
				if e != nil {
					return "", e
				}
				for _, d := range items {
					if d.MCPEnabled && (resource == strings.TrimRight(resourceBase, "/")+"/"+d.MCPRoute() || (d.ID == "default" && resource == resourceBase)) {
						return d.ID, nil
					}
				}
				return "", fmt.Errorf("未知资源")
			},
			KeyPath:    envflag.String("OAUTH_KEY_PATH", "data/oauth/signing.pem"),
			StatePath:  envflag.String("OAUTH_STATE_PATH", "data/oauth/state.sqlite3"),
			Password:   envflag.String("OAUTH_DEMO_PASSWORD", ""),
			DebugLogin: envflag.Bool("OAUTH_DEBUG_LOGIN", false),
		}, st)
		if err != nil {
			return err
		}
		defer as.Close()
		adminMCP, e := mcpserver.HTTPHandlerWithManagement(managementmcp.New(app), nil, "oauth", envflag.String("OAUTH_ISSUER", "http://127.0.0.1:8081"), adminResource, "/mcp/admin", true, manager, "")
		if e != nil {
			return e
		}
		routes := http.NewServeMux()
		routes.Handle("/mcp/admin", audit.HTTPHandler(manager.Audit, "management-mcp", adminMCP))
		routes.Handle("/.well-known/oauth-protected-resource/mcp/admin", adminMCP)
		routes.Handle("/", handler)
		handler = as.Handler(routes)
	}
	srv := &http.Server{
		Addr:              *addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	scanInterval, err := time.ParseDuration(envflag.String("DISCOVERY_INTERVAL", "5m"))
	if err != nil || scanInterval < 0 {
		return fmt.Errorf("DISCOVERY_INTERVAL 必须为非负时长")
	}
	discoveryDone := make(chan struct{})
	go func() { defer close(discoveryDone); catalogue.RunDiscovery(ctx, scanInterval) }()
	defer func() { stop(); <-discoveryDone }()
	outboxDone := make(chan struct{})
	go func() { defer close(outboxDone); platform.RunOutbox(ctx, manager) }()
	defer func() { stop(); <-outboxDone }()
	errCh := make(chan error, 1)
	go func() {
		logger.Info("serving admin API", "url", "http://"+*addr+"/api", "db", st.Path(), "mcp", *mcpURL)
		errCh <- srv.ListenAndServe()
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

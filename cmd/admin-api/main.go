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
	"syscall"
	"time"

	"sql-rewrite-server/internal/adminapi"
	"sql-rewrite-server/internal/envflag"
	"sql-rewrite-server/internal/mcpserver"
	"sql-rewrite-server/internal/oauth"
	"sql-rewrite-server/internal/rewrite"
	"sql-rewrite-server/internal/service"
	"sql-rewrite-server/internal/store"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server exited", "error", err)
		os.Exit(1)
	}
}

func run() error {
	dbPath := flag.String("db", envflag.String("SQLITE_PATH", "data/securities_demo.sqlite3"), "SQLite 数据库路径（只读打开）")
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

	st, err := store.Open(*dbPath, *queryTimeout)
	if err != nil {
		return err
	}
	defer st.Close()
	cfg, err := rewrite.LoadConfig(*rewriteConfig)
	if err != nil {
		return err
	}
	svc, err := service.New(context.Background(), st, cfg, asOfTime)
	if err != nil {
		return err
	}

	inspector := &adminapi.Inspector{
		Endpoint:    *mcpURL,
		Service:     svc,
		FixturesDir: *fixtures,
		UserHeader:  *userHeader,
		HTTPClient:  &http.Client{Timeout: 60 * time.Second},
	}
	handler := adminapi.Handler(svc, inspector)
	mode := envflag.String("MCP_AUTH_MODE", "oauth")
	if mode != "oauth" && mode != "header" && mode != "none" {
		return fmt.Errorf("invalid MCP_AUTH_MODE %q", mode)
	}
	if mode == "oauth" {
		as, err := oauth.New(oauth.Config{Issuer: envflag.String("OAUTH_ISSUER", "http://127.0.0.1:8081"), Resource: envflag.String("MCP_RESOURCE_URL", *mcpURL), KeyPath: envflag.String("OAUTH_KEY_PATH", "data/oauth/signing.pem"), StatePath: envflag.String("OAUTH_STATE_PATH", "data/oauth/state.sqlite3"), Password: envflag.String("OAUTH_DEMO_PASSWORD", ""), DebugLogin: envflag.Bool("OAUTH_DEBUG_LOGIN", false)}, st)
		if err != nil {
			return err
		}
		defer as.Close()
		inspector.Token = as.SignEmployee
		handler = as.Handler(handler)
	}
	srv := &http.Server{
		Addr:              *addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
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

// Command mcp-server serves the finance demo SQLite database over MCP.
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

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"sql-rewrite-server/internal/catalog"
	"sql-rewrite-server/internal/envflag"
	"sql-rewrite-server/internal/mcpserver"
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
	databaseID := flag.String("database", envflag.String("MCP_DATABASE", "default"), "stdio 连接的数据库 ID")
	transport := flag.String("transport", envflag.String("MCP_TRANSPORT", "http"), "传输方式：http（Streamable HTTP）或 stdio")
	addr := flag.String("addr", envflag.String("SERVER_ADDR", "127.0.0.1:8080"), "HTTP 监听地址")
	path := flag.String("path", envflag.String("MCP_PATH", "/mcp"), "MCP HTTP 端点路径")
	stateless := flag.Bool("stateless", envflag.Bool("MCP_STATELESS", true), "HTTP 无状态模式（不使用 Mcp-Session-Id）")
	queryTimeout := flag.Duration("query-timeout", envflag.Duration("QUERY_TIMEOUT", store.DefaultQueryTimeout), "单条查询超时")
	userHeader := flag.String("user-header", envflag.String("MCP_USER_HEADER", mcpserver.DefaultUserHeader), "网关传入可信用户编号的请求头")
	defaultUserID := flag.Int64("default-user-id", envflag.Int64("MCP_DEFAULT_USER_ID", 0), "请求头缺失时使用的用户编号（stdio 或本地调试）；0 表示拒绝")
	debug := flag.Bool("debug", envflag.Bool("MCP_DEBUG", false), "query 返回改写 SQL；管理模式工具启停由逐库配置控制")
	rewriteConfig := flag.String("rewrite-config", envflag.String("REWRITE_CONFIG", "config/rewrite.yml"), "SQL 改写规则配置文件")
	asOf := flag.String("as-of", envflag.String("REWRITE_AS_OF", store.DefaultAsOf.Format(time.RFC3339)), "授权过期判断的参考时间（RFC 3339）")
	authMode := envflag.String("MCP_AUTH_MODE", "oauth")
	issuer := envflag.String("OAUTH_ISSUER", "http://127.0.0.1:8081")
	resource := envflag.String("MCP_RESOURCE_URL", "http://127.0.0.1:8080/mcp")
	flag.Parse()
	if authMode != "oauth" && authMode != "header" && authMode != "none" {
		return fmt.Errorf("invalid MCP_AUTH_MODE %q", authMode)
	}
	if *transport == "stdio" && authMode != "none" {
		return fmt.Errorf("stdio requires MCP_AUTH_MODE=none and an explicit default employee")
	}
	if authMode == "none" && *defaultUserID <= 0 {
		return fmt.Errorf("none mode requires MCP_DEFAULT_USER_ID")
	}

	asOfTime, err := time.Parse(time.RFC3339, *asOf)
	if err != nil {
		return fmt.Errorf("-as-of: %w", err)
	}

	// stdout belongs to the protocol in stdio mode, so always log to stderr.
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	st, err := store.Open(*dbPath, *queryTimeout)
	if err != nil {
		return err
	}
	defer st.Close()
	catalogue, err := catalog.Bootstrap(envflag.String("DATABASE_CONFIG_DIR", "config/databases"), st, *rewriteConfig, asOfTime, *queryTimeout, envflag.String("DATABASE_CATALOG", "data/databases.json"))
	if err != nil {
		return err
	}
	initial, err := catalogue.Get("default")
	if err != nil {
		return err
	}
	svc, err := catalogue.Open(context.Background(), initial)
	if err != nil {
		return err
	}
	defer svc.Store.Close()

	opts := mcpserver.Options{AuthMode: authMode, UserHeader: *userHeader, DefaultUserID: *defaultUserID, Debug: *debug, IdentityStore: st}
	if !*stateless {
		logger.Warn("managed HTTP uses stateless requests so policy changes apply immediately")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch *transport {
	case "stdio":
		logger.Info("serving MCP over stdio", "db", st.Path(), "default_user_id", *defaultUserID)
		return mcpserver.ManagedServer(catalogue, *databaseID, svc, opts).Run(ctx, &mcp.StdioTransport{})
	case "http":
		handler, err := mcpserver.ManagedHTTPHandler(catalogue, st, opts, authMode, issuer, resource, *path)
		if err != nil {
			return err
		}
		return serveHTTP(ctx, logger, handler, st, *addr, *path)
	default:
		return fmt.Errorf("未知传输方式 %q，可选 http 或 stdio", *transport)
	}
}

func serveHTTP(ctx context.Context, logger *slog.Logger, handler http.Handler, st *store.Store, addr, path string) error {
	srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("serving MCP over Streamable HTTP", "url", "http://"+addr+path, "db", st.Path(), "stateless", true)
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

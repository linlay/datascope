// Package mcpserver exposes the finance demo SQLite database as MCP tools.
package mcpserver

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"sql-rewrite-server/internal/rewrite"
	"sql-rewrite-server/internal/service"
	"sql-rewrite-server/internal/store"
)

const (
	Name    = "sql-rewrite-server"
	Version = "0.3.0"

	DefaultUserHeader = "X-User-Id"
)

// Options configures identity and debug tools.
type Options struct {
	AuthMode string
	// UserHeader carries the trusted user id, set by the gateway in front of
	// this server. The AI never supplies identity.
	UserHeader string
	// DefaultUserID is used when the header is absent (e.g. stdio). Zero
	// means no default: requests without identity are rejected.
	DefaultUserID int64
	// Debug registers raw_query and customer_scope and returns the rewritten
	// SQL from query. Never enable in production.
	Debug bool
}

const instructions = `证券经纪业务样例库（SQLite 方言，全部为合成数据，金额单位为整数分，日期为 YYYY-MM-DD，交易时间为中国时区 RFC3339）。
建议流程：先 list_tables 了解表、中文注释和访问方案，再 describe_table 查看字段和外键，最后用 query 执行 SELECT。
query 会按当前用户的权限自动改写 SQL：FROM / JOIN 中的每张表都会被替换为该用户可见数据的子查询，因此直接写标准 SQL 即可，无需自行添加权限过滤。
访问方案为 RESTRICTED 的表（管理范围）仅超级管理员可查询。`

// New builds an MCP server backed by svc.
func New(svc *service.QueryService, opts Options) *mcp.Server {
	if opts.UserHeader == "" {
		opts.UserHeader = DefaultUserHeader
	}
	server := mcp.NewServer(&mcp.Implementation{
		Name:    Name,
		Title:   "证券业务 SQL 查询（权限改写）",
		Version: Version,
	}, &mcp.ServerOptions{
		Instructions: instructions,
		// Only tools are offered; drop the SDK's historical default logging capability.
		Capabilities: &mcp.ServerCapabilities{},
	})
	h := handlers{svc: svc, opts: opts}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_tables",
		Title:       "列出数据表",
		Description: "列出样例库中的全部数据表，包含中文表注释和访问方案（NONE 公共字典 / EMPLOYEE 员工权限 / CUSTOMER 客户权限 / RESTRICTED 仅超级管理员）。",
		Annotations: readOnly("列出数据表"),
	}, h.listTables)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "describe_table",
		Title:       "查看表结构",
		Description: "返回指定表的字段、类型、主键、外键引用、索引、中文字段注释、建表语句及访问方案。",
		Annotations: readOnly("查看表结构"),
	}, h.describeTable)

	mcp.AddTool(server, &mcp.Tool{
		Name:  "query",
		Title: "执行 SQL（按权限改写）",
		Description: fmt.Sprintf("执行单条 SELECT（SQLite 方言）。服务端会按当前用户权限把 FROM / JOIN 中的每张表改写为带权限的子查询，"+
			"结果只包含该用户可见的数据。不支持写操作、多语句、表值函数、schema 限定表名和 “IN 表名” 写法。默认最多返回 %d 行，上限 %d 行。",
			store.DefaultMaxRows, store.HardMaxRows),
		Annotations: readOnly("执行 SQL（按权限改写）"),
	}, h.query)

	if opts.Debug {
		mcp.AddTool(server, &mcp.Tool{
			Name:  "raw_query",
			Title: "原始 SQL（调试）",
			Description: "调试专用：不做任何改写，按原样执行传入的单条 SQL，可看到全部数据。数据库以只读方式打开。" +
				"生产环境不开放此工具。",
			Annotations: readOnly("原始 SQL（调试）"),
		}, h.rawQuery)

		mcp.AddTool(server, &mcp.Tool{
			Name:  "customer_scope",
			Title: "计算客户可见范围（调试）",
			Description: "调试专用：按参考权限规则计算任意用户可见的客户编号，用于核对 query 的改写结果。" +
				"规则：在职员工的名下客户，加上生效管理范围内客户；管理员可见全部。",
			Annotations: readOnly("计算客户可见范围（调试）"),
		}, h.customerScope)
	}

	return server
}

func readOnly(title string) *mcp.ToolAnnotations {
	closed := false
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &closed}
}

type handlers struct {
	svc  *service.QueryService
	opts Options
}

// userID resolves the trusted identity: the gateway header, else the
// configured default. Tool arguments are never consulted.
func (h handlers) userID(req *mcp.CallToolRequest) (int64, error) {
	if h.opts.AuthMode == "oauth" {
		if req.Extra == nil || req.Extra.TokenInfo == nil {
			return 0, fmt.Errorf("缺少 OAuth 身份")
		}
		id, err := strconv.ParseInt(req.Extra.TokenInfo.UserID, 10, 64)
		if err != nil || id <= 0 {
			return 0, fmt.Errorf("无效 OAuth 身份")
		}
		return id, nil
	}
	if h.opts.AuthMode != "none" && req.Extra != nil && req.Extra.Header != nil {
		if v := req.Extra.Header.Get(h.opts.UserHeader); v != "" {
			id, err := strconv.ParseInt(v, 10, 64)
			if err != nil || id <= 0 {
				return 0, fmt.Errorf("请求头 %s 不是合法的用户编号", h.opts.UserHeader)
			}
			return id, nil
		}
	}
	if h.opts.DefaultUserID > 0 {
		return h.opts.DefaultUserID, nil
	}
	return 0, fmt.Errorf("缺少可信用户身份：需由网关在请求头 %s 中传入用户编号", h.opts.UserHeader)
}

// Access is a table's rewrite scheme as shown to the model.
type Access struct {
	Scheme rewrite.Scheme `json:"scheme"`
	Name   string         `json:"name"`
}

// AccessFor returns the access scheme reported for a table.
func AccessFor(cfg *rewrite.Config, table string) Access {
	pol, ok := cfg.Lookup(table)
	if !ok {
		return Access{}
	}
	return Access{Scheme: pol.Scheme, Name: cfg.SchemeName(pol.Scheme)}
}

type listTablesInput struct{}

type tableEntry struct {
	Name    string `json:"name"`
	Comment string `json:"comment"`
	Access  Access `json:"access"`
}

type listTablesOutput struct {
	Tables []tableEntry `json:"tables"`
}

func (h handlers) listTables(ctx context.Context, _ *mcp.CallToolRequest, _ listTablesInput) (*mcp.CallToolResult, listTablesOutput, error) {
	tables, err := h.svc.Store.Tables(ctx)
	if err != nil {
		return nil, listTablesOutput{}, err
	}
	out := listTablesOutput{Tables: make([]tableEntry, len(tables))}
	for i, t := range tables {
		// Row counts are global, not per user, so they are not exposed here.
		out.Tables[i] = tableEntry{Name: t.Name, Comment: t.Comment, Access: AccessFor(h.svc.Config(), t.Name)}
	}
	return nil, out, nil
}

type describeTableInput struct {
	Table string `json:"table" jsonschema:"表名，例如 customers；可先调用 list_tables 获取"`
}

type describeTableOutput struct {
	*store.TableSchema
	Access Access `json:"access"`
}

func (h handlers) describeTable(ctx context.Context, _ *mcp.CallToolRequest, in describeTableInput) (*mcp.CallToolResult, *describeTableOutput, error) {
	schema, err := h.svc.Store.Describe(ctx, in.Table)
	if err != nil {
		return nil, nil, err
	}
	return nil, &describeTableOutput{TableSchema: schema, Access: AccessFor(h.svc.Config(), schema.Name)}, nil
}

type queryInput struct {
	SQL     string `json:"sql" jsonschema:"单条 SELECT 语句（SQLite 方言），直接写标准 SQL，无需添加权限过滤"`
	MaxRows int    `json:"max_rows,omitempty" jsonschema:"最多返回的行数，默认 200，上限 1000"`
}

type queryOutput struct {
	*store.QueryResult
	Tables []rewrite.TableRef `json:"tables"`
	// Only in debug mode.
	UserID       int64          `json:"user_id,omitempty"`
	RewrittenSQL string         `json:"rewritten_sql,omitempty"`
	Binds        map[string]any `json:"binds,omitempty"`
}

func (h handlers) query(ctx context.Context, req *mcp.CallToolRequest, in queryInput) (*mcp.CallToolResult, *queryOutput, error) {
	uid, err := h.userID(req)
	if err != nil {
		return nil, nil, err
	}
	res, err := h.svc.Query(ctx, in.SQL, uid, in.MaxRows)
	if err != nil {
		return nil, nil, err
	}
	out := &queryOutput{QueryResult: res.QueryResult, Tables: res.Tables}
	if h.opts.Debug {
		out.UserID, out.RewrittenSQL, out.Binds = uid, res.RewrittenSQL, res.Binds
	}
	return nil, out, nil
}

type rawQueryInput struct {
	SQL     string `json:"sql" jsonschema:"任意单条 SQL，按原样执行，不做权限改写"`
	MaxRows int    `json:"max_rows,omitempty" jsonschema:"最多返回的行数，默认 200，上限 1000"`
}

func (h handlers) rawQuery(ctx context.Context, req *mcp.CallToolRequest, in rawQueryInput) (*mcp.CallToolResult, *store.QueryResult, error) {
	if err := h.debugAllowed(ctx, req); err != nil {
		return nil, nil, err
	}
	result, err := h.svc.Store.RawQuery(ctx, in.SQL, in.MaxRows)
	if err != nil {
		return nil, nil, err
	}
	return nil, result, nil
}

type customerScopeInput struct {
	UserID int64  `json:"user_id" jsonschema:"系统用户编号，例如 1（客户经理）、121（营业部主管）、175（管理员）"`
	AsOf   string `json:"as_of,omitempty" jsonschema:"参考时间，RFC 3339 格式，默认与改写使用的参考时间相同"`
}

type customerScopeOutput struct {
	UserID      int64    `json:"user_id"`
	AsOf        string   `json:"as_of"`
	CustomerIDs []string `json:"customer_ids"`
}

func (h handlers) customerScope(ctx context.Context, req *mcp.CallToolRequest, in customerScopeInput) (*mcp.CallToolResult, customerScopeOutput, error) {
	if err := h.debugAllowed(ctx, req); err != nil {
		return nil, customerScopeOutput{}, err
	}
	asOf := h.svc.AsOf
	if in.AsOf != "" {
		parsed, err := time.Parse(time.RFC3339, in.AsOf)
		if err != nil {
			return nil, customerScopeOutput{}, fmt.Errorf("as_of 不是合法的 RFC 3339 时间: %w", err)
		}
		asOf = parsed
	}
	ids, err := h.svc.Store.VisibleCustomerIDs(ctx, in.UserID, asOf)
	if err != nil {
		return nil, customerScopeOutput{}, err
	}
	return nil, customerScopeOutput{UserID: in.UserID, AsOf: asOf.UTC().Format(time.RFC3339), CustomerIDs: ids}, nil
}

func (h handlers) debugAllowed(ctx context.Context, req *mcp.CallToolRequest) error {
	if h.opts.AuthMode == "oauth" {
		if req.Extra == nil || req.Extra.TokenInfo == nil || !slices.Contains(req.Extra.TokenInfo.Scopes, "sql:debug") {
			return fmt.Errorf("需要 sql:debug 权限")
		}
		return nil
	}
	uid, err := h.userID(req)
	if err != nil {
		return err
	}
	users, err := h.svc.Store.Users(ctx)
	if err != nil {
		return err
	}
	for _, u := range users {
		if u.ID == uid && u.Status == "active" && u.RoleCode == "ADMIN" {
			return nil
		}
	}
	return fmt.Errorf("仅在职管理员可使用调试工具")
}

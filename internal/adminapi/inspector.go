package adminapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"sql-rewrite-server/internal/mcpserver"
	"sql-rewrite-server/internal/service"
	"sql-rewrite-server/internal/store"
)

// Inspector talks to the MCP server as a real MCP client and compares what it
// returns against direct reads of the same database.
type Inspector struct {
	Endpoint    string
	Service     *service.QueryService
	FixturesDir string // permission_cases.json and rewrite_cases.json; optional
	// UserHeader is the header the MCP server trusts for identity; the
	// inspector sets it the way a gateway would.
	UserHeader       string
	Token            func(context.Context, int64) (string, error)
	TokenForDatabase func(context.Context, int64, string) (string, error)
	HTTPClient       *http.Client
}

// userHeaderTransport plays the gateway: it stamps the trusted user header.
type userHeaderTransport struct {
	base   http.RoundTripper
	header string
	userID int64
	token  string
}

func (t userHeaderTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	if t.token != "" {
		r.Header.Set("Authorization", "Bearer "+t.token)
	} else {
		r.Header.Set(t.header, strconv.FormatInt(t.userID, 10))
	}
	return t.base.RoundTrip(r)
}

// connect opens a session; userID 0 sends no identity header.
func (in *Inspector) connect(ctx context.Context, userID int64) (*mcp.ClientSession, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	if in.HTTPClient != nil {
		*client = *in.HTTPClient
	}
	if in.Token != nil && userID == 0 {
		userID = superAdminID
	}
	if userID != 0 {
		base := client.Transport
		if base == nil {
			base = http.DefaultTransport
		}
		token := ""
		if in.Token != nil {
			var err error
			token, err = in.Token(ctx, userID)
			if err != nil {
				return nil, err
			}
		}
		client.Transport = userHeaderTransport{token: token, base: base, header: cmpOr(in.UserHeader, mcpserver.DefaultUserHeader), userID: userID}
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "sql-rewrite-admin-api", Version: mcpserver.Version}, nil).
		Connect(ctx, &mcp.StreamableClientTransport{Endpoint: in.Endpoint, HTTPClient: client, MaxRetries: -1, DisableStandaloneSSE: true}, nil)
	if err != nil {
		return nil, fmt.Errorf("无法连接 MCP server %s: %w", in.Endpoint, err)
	}
	return session, nil
}

func cmpOr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// ServerInfo is what a client sees after connecting.
type ServerInfo struct {
	Endpoint              string                  `json:"endpoint"`
	ProtocolVersion       string                  `json:"protocol_version"`
	LatestProtocolVersion string                  `json:"latest_protocol_version"`
	Server                *mcp.Implementation     `json:"server"`
	Instructions          string                  `json:"instructions"`
	Capabilities          *mcp.ServerCapabilities `json:"capabilities"`
	Tools                 []*mcp.Tool             `json:"tools"`
	AuthMode              string                  `json:"auth_mode"`
	UserHeader            string                  `json:"user_header"`
	ElapsedMS             int64                   `json:"elapsed_ms"`
}

func (in *Inspector) Info(ctx context.Context) (*ServerInfo, error) {
	start := time.Now()
	session, err := in.connect(ctx, 0)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		return nil, err
	}
	init := session.InitializeResult()
	return &ServerInfo{
		Endpoint:              in.Endpoint,
		ProtocolVersion:       init.ProtocolVersion,
		LatestProtocolVersion: mcp.SupportedProtocolVersions()[0],
		Server:                init.ServerInfo,
		Instructions:          init.Instructions,
		Capabilities:          init.Capabilities,
		Tools:                 tools.Tools,
		AuthMode: func() string {
			if in.Token != nil {
				return "oauth"
			}
			return "header"
		}(),
		UserHeader: cmpOr(in.UserHeader, mcpserver.DefaultUserHeader),
		ElapsedMS:  time.Since(start).Milliseconds(),
	}, nil
}

// CallResult is one raw tools/call round trip.
type CallResult struct {
	Tool      string              `json:"tool"`
	UserID    int64               `json:"user_id"`
	Arguments map[string]any      `json:"arguments"`
	Result    *mcp.CallToolResult `json:"result"`
	ElapsedMS int64               `json:"elapsed_ms"`
}

// Call invokes a tool as userID (0 sends no identity header).
func (in *Inspector) Call(ctx context.Context, userID int64, name string, args map[string]any) (*CallResult, error) {
	if name == "" {
		return nil, errors.New("缺少工具名称")
	}
	if args == nil {
		args = map[string]any{}
	}
	start := time.Now()
	session, err := in.connect(ctx, userID)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return nil, err
	}
	return &CallResult{Tool: name, UserID: userID, Arguments: args, Result: res, ElapsedMS: time.Since(start).Milliseconds()}, nil
}

// Check statuses.
const (
	StatusPass = "pass"
	StatusFail = "fail"
	StatusWarn = "warn"
	StatusSkip = "skip"
)

type Check struct {
	Group    string `json:"group"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	Detail   string `json:"detail,omitempty"`
	Expected any    `json:"expected,omitempty"`
	Actual   any    `json:"actual,omitempty"`
}

type Report struct {
	Endpoint        string   `json:"endpoint"`
	ProtocolVersion string   `json:"protocol_version"`
	DebugTools      []string `json:"debug_tools"`
	Passed          int      `json:"passed"`
	Failed          int      `json:"failed"`
	Warned          int      `json:"warned"`
	Skipped         int      `json:"skipped"`
	Checks          []Check  `json:"checks"`
	ElapsedMS       int64    `json:"elapsed_ms"`
}

func (r *Report) add(c Check) {
	switch c.Status {
	case StatusPass:
		r.Passed++
		// Keep the payload small: values are only interesting on failure.
		c.Expected, c.Actual = nil, nil
	case StatusFail:
		r.Failed++
	case StatusWarn:
		r.Warned++
	default:
		r.Skipped++
	}
	r.Checks = append(r.Checks, c)
}

func (r *Report) compare(group, name string, expected, actual any, err error, okDetail string) {
	switch {
	case err != nil:
		r.add(Check{Group: group, Name: name, Status: StatusFail, Detail: err.Error(), Expected: normalize(expected)})
	case reflect.DeepEqual(normalize(expected), normalize(actual)):
		r.add(Check{Group: group, Name: name, Status: StatusPass, Detail: okDetail})
	default:
		r.add(Check{Group: group, Name: name, Status: StatusFail, Detail: "MCP 返回与预期不一致", Expected: normalize(expected), Actual: normalize(actual)})
	}
}

func (r *Report) expectRejected(group, name string, res *mcp.CallToolResult, err error) {
	switch {
	case err != nil:
		r.add(Check{Group: group, Name: name, Status: StatusFail, Detail: err.Error()})
	case res.IsError:
		r.add(Check{Group: group, Name: name, Status: StatusPass, Detail: "已拒绝：" + toolText(res)})
	default:
		r.add(Check{Group: group, Name: name, Status: StatusFail, Detail: "未被拒绝", Actual: normalize(res.StructuredContent)})
	}
}

const superAdminID = 175

var (
	coreTools  = []string{"describe_table", "list_tables", "query"}
	debugTools = []string{"customer_scope", "raw_query"}
)

var sampleQueries = []struct{ name, sql string }{
	{"客户总数", "SELECT COUNT(*) AS n FROM customers"},
	{"各营业部客户数", "SELECT department_code,COUNT(DISTINCT customer_no) AS n FROM fund_accounts GROUP BY department_code ORDER BY department_code"},
	{"证券成交 JOIN", "SELECT t.id,c.name,s.code,t.amount_cents FROM trades t JOIN customers c ON c.customer_no=t.customer_no JOIN securities s ON s.code=t.security_code ORDER BY t.id LIMIT 20"},
	{"CTE 资金汇总", "WITH s AS (SELECT customer_no,SUM(amount_cents) AS total FROM cash_flows GROUP BY customer_no) SELECT * FROM s ORDER BY customer_no LIMIT 10"},
	{"员工范围", "SELECT employee_id,emp_no,name,status FROM employees ORDER BY employee_id"},
}

var forbiddenQueries = []struct{ name, sql string }{
	{"拒绝 DELETE", "DELETE FROM customers"},
	{"拒绝 PRAGMA", "PRAGMA writable_schema=ON"},
	{"拒绝多语句", "SELECT 1; DROP TABLE users"},
	{"拒绝 CTE 写入", "WITH x AS (SELECT 1) DELETE FROM customers"},
	{"拒绝表值函数", "SELECT * FROM pragma_table_info('users')"},
	{"拒绝 IN 表名", "SELECT 1 WHERE 1 IN customers"},
	{"拒绝 schema 限定", "SELECT * FROM main.customers"},
	{"拒绝 CTE 覆盖表名", "WITH customers AS (SELECT 1 AS id) SELECT * FROM customers"},
}

// sessions caches one MCP session per identity during a verification run.
type sessions struct {
	in    *Inspector
	ctx   context.Context
	byUID map[int64]*mcp.ClientSession
}

func (s *sessions) get(userID int64) (*mcp.ClientSession, error) {
	if cs, ok := s.byUID[userID]; ok {
		return cs, nil
	}
	cs, err := s.in.connect(s.ctx, userID)
	if err != nil {
		return nil, err
	}
	s.byUID[userID] = cs
	return cs, nil
}

func (s *sessions) close() {
	for _, cs := range s.byUID {
		cs.Close()
	}
}

func (s *sessions) call(userID int64, name string, args map[string]any) (*mcp.CallToolResult, error) {
	cs, err := s.get(userID)
	if err != nil {
		return nil, err
	}
	return cs.CallTool(s.ctx, &mcp.CallToolParams{Name: name, Arguments: args})
}

func (s *sessions) structured(userID int64, name string, args map[string]any) (any, error) {
	res, err := s.call(userID, name, args)
	if err != nil {
		return nil, err
	}
	if res.IsError {
		return nil, fmt.Errorf("工具返回错误：%s", toolText(res))
	}
	return res.StructuredContent, nil
}

// Verify connects to the MCP server and checks protocol, tool catalog, data
// consistency, permission rewriting and read-only enforcement.
func (in *Inspector) Verify(ctx context.Context) (*Report, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	ss := &sessions{in: in, ctx: ctx, byUID: map[int64]*mcp.ClientSession{}}
	defer ss.close()
	anon, err := ss.get(0)
	if err != nil {
		return nil, err
	}
	st := in.Service.Store

	report := &Report{Endpoint: in.Endpoint, ProtocolVersion: anon.InitializeResult().ProtocolVersion, DebugTools: []string{}, Checks: []Check{}}
	latest := mcp.SupportedProtocolVersions()[0]
	report.compare("协议", "协商到最新 MCP 协议版本", latest, report.ProtocolVersion, nil, latest)

	// Tool catalog.
	tools, err := anon.ListTools(ctx, nil)
	if err != nil {
		return nil, err
	}
	var names, problems []string
	for _, t := range tools.Tools {
		names = append(names, t.Name)
		if t.Annotations == nil || !t.Annotations.ReadOnlyHint {
			problems = append(problems, t.Name+" 未标注 readOnlyHint")
		}
		if t.OutputSchema == nil {
			problems = append(problems, t.Name+" 缺少 outputSchema")
		}
	}
	var missing []string
	for _, name := range coreTools {
		if !slices.Contains(names, name) {
			missing = append(missing, name)
		}
	}
	for _, name := range debugTools {
		if slices.Contains(names, name) {
			report.DebugTools = append(report.DebugTools, name)
		}
	}
	if len(missing) == 0 {
		report.add(Check{Group: "协议", Name: "tools/list 核心工具", Status: StatusPass, Detail: strings.Join(coreTools, "、")})
	} else {
		report.add(Check{Group: "协议", Name: "tools/list 核心工具", Status: StatusFail, Detail: "缺少：" + strings.Join(missing, "、")})
	}
	if len(problems) == 0 {
		report.add(Check{Group: "协议", Name: "工具标注与输出 schema", Status: StatusPass, Detail: "全部只读且声明 outputSchema"})
	} else {
		report.add(Check{Group: "协议", Name: "工具标注与输出 schema", Status: StatusFail, Detail: strings.Join(problems, "；")})
	}
	debug := len(report.DebugTools) == len(debugTools)
	if debug {
		report.add(Check{Group: "协议", Name: "调试工具", Status: StatusWarn, Detail: "调试模式已开启：raw_query 仅管理员可用，生产环境应关闭"})
	} else {
		report.add(Check{Group: "协议", Name: "调试工具", Status: StatusPass, Detail: "未开放 raw_query / customer_scope"})
	}

	// Schema tools return the same as the database.
	tables, err := st.Tables(ctx)
	if err != nil {
		return nil, err
	}
	expectedTables := make([]map[string]any, len(tables))
	for i, t := range tables {
		expectedTables[i] = map[string]any{"name": t.Name, "comment": t.Comment, "access": mcpserver.AccessFor(in.Service.Config(), t.Name)}
	}
	actual, err := ss.structured(0, "list_tables", map[string]any{})
	report.compare("数据一致性", "list_tables", map[string]any{"tables": expectedTables}, actual, err, fmt.Sprintf("%d 张表及访问方案", len(tables)))

	var mismatched []string
	for _, t := range tables {
		schema, err := st.Describe(ctx, t.Name)
		if err != nil {
			return nil, err
		}
		expected := normalize(schema).(map[string]any)
		expected["access"] = mcpserver.AccessFor(in.Service.Config(), t.Name)
		actual, err := ss.structured(0, "describe_table", map[string]any{"table": t.Name})
		if err != nil || !reflect.DeepEqual(normalize(expected), normalize(actual)) {
			mismatched = append(mismatched, t.Name)
		}
	}
	if len(mismatched) == 0 {
		report.add(Check{Group: "数据一致性", Name: "describe_table（逐表）", Status: StatusPass, Detail: fmt.Sprintf("%d 张表结构一致", len(tables))})
	} else {
		report.add(Check{Group: "数据一致性", Name: "describe_table（逐表）", Status: StatusFail, Detail: "不一致：" + strings.Join(mismatched, "、")})
	}

	// raw_query and super-admin query both see everything, so both must equal
	// a direct read.
	for _, q := range sampleQueries {
		expected, err := st.RawQuery(ctx, q.sql, store.HardMaxRows)
		if err != nil {
			return nil, err
		}
		if debug {
			actual, err := ss.structured(superAdminID, "raw_query", map[string]any{"sql": q.sql, "max_rows": store.HardMaxRows})
			report.compare("原始查询", "raw_query："+q.name, expected, actual, err, fmt.Sprintf("%d 行一致", expected.RowCount))
		} else {
			report.add(Check{Group: "原始查询", Name: "raw_query：" + q.name, Status: StatusSkip, Detail: "MCP server 未开启调试模式"})
		}
		actual, err := ss.structured(superAdminID, "query", map[string]any{"sql": q.sql, "max_rows": store.HardMaxRows})
		var rows any
		if m, ok := actual.(map[string]any); ok {
			rows = map[string]any{"columns": m["columns"], "rows": m["rows"], "row_count": m["row_count"], "truncated": m["truncated"]}
		}
		report.compare("权限改写", "超级管理员 query = 原始数据："+q.name, expected, rows, err, fmt.Sprintf("%d 行一致", expected.RowCount))
	}

	if err := in.verifyRewriteCases(ss, report); err != nil {
		return nil, err
	}
	if err := in.verifyPermissionCases(ss, report); err != nil {
		return nil, err
	}

	// Safety.
	for _, q := range forbiddenQueries {
		res, err := ss.call(superAdminID, "query", map[string]any{"sql": q.sql})
		report.expectRejected("只读安全", "query "+q.name, res, err)
	}
	if in.Token != nil {
		req, _ := http.NewRequestWithContext(ctx, "POST", in.Endpoint, nil)
		client := &http.Client{Timeout: 10 * time.Second}
		response, err := client.Do(req)
		if err != nil {
			report.add(Check{Group: "只读安全", Name: "无令牌请求被拒绝", Status: StatusFail, Detail: err.Error()})
		} else {
			response.Body.Close()
			if response.StatusCode == 401 && strings.Contains(response.Header.Get("WWW-Authenticate"), "resource_metadata=") {
				report.add(Check{Group: "只读安全", Name: "无令牌请求被拒绝", Status: StatusPass, Detail: "401 与 OAuth 资源元数据发现地址"})
			} else {
				report.add(Check{Group: "只读安全", Name: "无令牌请求被拒绝", Status: StatusFail, Detail: response.Status})
			}
		}
	} else {
		res, err := ss.call(0, "query", map[string]any{"sql": "SELECT customer_no FROM customers"})
		switch {
		case err != nil:
			report.add(Check{Group: "只读安全", Name: "缺少身份请求头时拒绝 query", Status: StatusFail, Detail: err.Error()})
		case res.IsError:
			report.add(Check{Group: "只读安全", Name: "缺少身份请求头时拒绝 query", Status: StatusPass, Detail: toolText(res)})
		default:
			report.add(Check{Group: "只读安全", Name: "缺少身份请求头时拒绝 query", Status: StatusWarn, Detail: "MCP server 配置了默认用户（MCP_DEFAULT_USER_ID），未带身份的请求会以该用户执行"})
		}
	}

	if debug {
		res, err := ss.call(superAdminID, "raw_query", map[string]any{"sql": "DELETE FROM customers"})
		report.expectRejected("只读安全", "raw_query 无法写入（只读连接）", res, err)
	}

	report.ElapsedMS = time.Since(start).Milliseconds()
	return report, nil
}

func (in *Inspector) readFixture(name string, v any) error {
	raw, err := os.ReadFile(filepath.Join(in.FixturesDir, name))
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}

func customerIDColumn(out any) any {
	m, ok := out.(map[string]any)
	if !ok {
		return nil
	}
	rows, _ := m["rows"].([]any)
	ids := []any{}
	for _, row := range rows {
		if r, ok := row.(map[string]any); ok {
			ids = append(ids, r["customer_no"])
		}
	}
	return ids
}

// verifyRewriteCases runs fixtures/rewrite_cases.json through MCP query.
func (in *Inspector) verifyRewriteCases(ss *sessions, report *Report) error {
	var cases []struct {
		ID           string           `json:"id"`
		UserID       int64            `json:"user_id"`
		SQL          string           `json:"sql"`
		ExpectedIDs  []string         `json:"expected_customer_ids"`
		ExpectedRows []map[string]any `json:"expected_rows"`
		ExpectedErr  string           `json:"expected_error"`
		ExpectedRule string           `json:"expected_rule"`
	}
	if err := in.readFixture("rewrite_cases.json", &cases); err != nil {
		report.add(Check{Group: "改写验收", Name: "rewrite_cases.json", Status: StatusSkip, Detail: "未读取到改写验收案例：" + err.Error()})
		return nil
	}
	for _, c := range cases {
		name := fmt.Sprintf("%s（员工 %d）", c.ID, c.UserID)
		if in.checkInactiveIdentity(ss.ctx, report, "改写验收", name, c.UserID) {
			continue
		}
		args := map[string]any{"sql": c.SQL, "max_rows": store.HardMaxRows}
		switch {
		case c.ExpectedErr != "":
			res, err := ss.call(c.UserID, "query", args)
			report.expectRejected("改写验收", name, res, err)
		case c.ExpectedRule != "":
			report.add(Check{Group: "改写验收", Name: name, Status: StatusSkip, Detail: "字段脱敏尚未实现：" + c.ExpectedRule})
		case c.ExpectedRows != nil:
			out, err := ss.structured(c.UserID, "query", args)
			var rows any
			if m, ok := out.(map[string]any); ok {
				rows = m["rows"]
			}
			report.compare("改写验收", name, c.ExpectedRows, rows, err, c.SQL)
		default:
			out, err := ss.structured(c.UserID, "query", args)
			report.compare("改写验收", name, c.ExpectedIDs, customerIDColumn(out), err, c.SQL)
		}
	}
	return nil
}

// verifyPermissionCases checks MCP query against the hand-written expected
// customer ids, which are independent of the Go and SQL implementations.
func (in *Inspector) verifyPermissionCases(ss *sessions, report *Report) error {
	var cases []struct {
		ID          string   `json:"id"`
		UserID      int64    `json:"user_id"`
		Expected    []string `json:"expected_customer_ids"`
		Description string   `json:"description"`
	}
	if err := in.readFixture("permission_cases.json", &cases); err != nil {
		report.add(Check{Group: "权限案例", Name: "permission_cases.json", Status: StatusSkip, Detail: "未读取到权限案例：" + err.Error()})
		return nil
	}
	for _, c := range cases {
		if in.checkInactiveIdentity(ss.ctx, report, "权限案例", c.ID, c.UserID) {
			continue
		}
		out, err := ss.structured(c.UserID, "query", map[string]any{"sql": "SELECT customer_no FROM customers ORDER BY customer_no", "max_rows": store.HardMaxRows})
		report.compare("权限案例", fmt.Sprintf("%s（用户 %d）", c.ID, c.UserID), c.Expected, customerIDColumn(out), err, c.Description)
	}
	return nil
}

func toolText(res *mcp.CallToolResult) string {
	var parts []string
	for _, c := range res.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, t.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// normalize round-trips v through JSON so Go values and decoded MCP payloads
// compare equal when they would serialize identically.
func normalize(v any) any {
	raw, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return v
	}
	return out
}

// OAuth cannot impersonate resigned or missing employees. Verify rejection at
// credential issuance; the SQL workbench and service tests check their empty scopes.
func (in *Inspector) checkInactiveIdentity(ctx context.Context, report *Report, group, name string, uid int64) bool {
	if in.Token == nil {
		return false
	}
	employees, err := in.Service.Store.Users(ctx)
	if err != nil {
		return false
	}
	for _, u := range employees {
		if u.ID == uid && u.Status == "active" {
			return false
		}
	}
	_, err = in.Token(ctx, uid)
	if err != nil {
		report.add(Check{Group: group, Name: name, Status: StatusPass, Detail: "离职或未知员工已拒绝签发 OAuth 令牌"})
	} else {
		report.add(Check{Group: group, Name: name, Status: StatusFail, Detail: "离职或未知员工不应获发令牌"})
	}
	return true
}

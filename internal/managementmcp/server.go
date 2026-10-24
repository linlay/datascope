// Package managementmcp exposes domain actions for this platform, never arbitrary internal SQL.
package managementmcp

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"datascope-server/internal/application"
	"datascope-server/internal/audit"
	"datascope-server/internal/guard"
	"datascope-server/internal/management"
	"strconv"
)

func New(app *application.Service) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "datascope-management", Version: "1.0.0", Title: "DataScope 管理平台"}, &mcp.ServerOptions{Instructions: "管理本平台的用户、数据库、授权、访问策略和业务知识。先读取当前配置及版本，验证后保存。管理 SQLite 不提供任意 SQL；业务数据库查询使用各数据库独立 MCP。", Capabilities: &mcp.ServerCapabilities{}})
	descriptions := map[string]string{
		"enable_user": "重新启用 user_id，旧会话和旧令牌仍保持撤销。",
		"list_users":  "列出平台用户，需要 users.manage。", "create_user": "创建用户，需要 username、name、password（至少12字符），不自动获得任何权限。", "disable_user": "禁用 user_id 并撤销登录会话。", "reset_password": "重置 user_id 的密码并撤销会话。",
		"get_knowledge":   "读取 database_id、id 的知识详情，包括草稿；需要 knowledge.write。",
		"test_connection": "测试 database_id 的连接和策略是否有效。",
		"update_database": "更新 database_id 的显示名称，需要 database 与最新 revision。",
		"validate_policy": "验证 database_id 的 database.config，不保存，不执行查询。",
		"save_policy":     "保存 database_id 的 database.config，需要最新 revision。",
		"configure_mcp":   "设置 database_id 的 MCP 路径、tools 与 business_tools，需要 database 及最新 revision。",
		"list_databases":  "列出自己可管理的数据库配置。", "create_database": "登记服务端已有 SQLite 文件，需要 database；默认关闭 MCP 和所有数据表。", "get_database": "读取 database_id 的完整配置与 revision。", "save_database": "保存 database_id 的完整 database 配置，保留最新 revision。",
		"list_database_grants": "列出 database_id 的用户授权。", "grant_database_permissions": "为 database_id、user_id 设置完整 permissions 列表及 business_user_id，后者仅系统管理员可修改。", "revoke_database_permissions": "撤销 database_id、user_id 的全部权限。",
		"get_policy": "读取 database_id 的访问策略。", "preview_rewrite": "预览 database_id 的 sql 权限改写；business_user_id 模拟需要 impersonate，不执行查询。",
		"search_audit_events": "分页读取 database_id 的审计；可按 request_id 关联 SQL。", "list_knowledge": "列出 database_id 的知识摘要，包括草稿。", "save_knowledge": "创建或更新知识：database_id、id、name、summary、content、tables、published、revision；更新须带最新版本。",
		"search_knowledge": "按 database_id 和 query 检索已发布业务知识摘要。", "read_knowledge": "按 database_id 和 id 读取已发布知识正文。",
	}
	for name, description := range descriptions {
		action := name
		readOnly := name == "list_users" || name == "list_databases" || name == "get_database" || name == "get_policy" || name == "get_knowledge" || name == "test_connection" || name == "validate_policy" || name == "list_database_grants" || name == "preview_rewrite" || name == "search_audit_events" || name == "list_knowledge" || name == "search_knowledge" || name == "read_knowledge"
		mcp.AddTool(server, &mcp.Tool{Name: name, Description: description, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: readOnly}}, func(ctx context.Context, req *mcp.CallToolRequest, in application.Input) (*mcp.CallToolResult, any, error) {
			if req.Extra == nil || req.Extra.TokenInfo == nil {
				return nil, nil, management.ErrDenied
			}
			uid, e := strconv.ParseInt(req.Extra.TokenInfo.UserID, 10, 64)
			if e != nil {
				return nil, nil, e
			}
			u, e := app.Management.User(ctx, uid)
			if e != nil || !u.Active {
				return nil, nil, management.ErrDenied
			}
			ctx = guard.WithUser(audit.With(ctx, audit.Metadata{RequestID: audit.From(ctx).RequestID, ActorID: u.ID, DatabaseID: in.DatabaseID, Source: "management-mcp", Tool: action}), u)
			out, e := app.Call(ctx, action, in)
			return nil, out, e
		})
	}
	return server
}

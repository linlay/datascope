package mcpserver

import (
	"context"
	"datascope-server/internal/application"
	"datascope-server/internal/audit"
	"datascope-server/internal/guard"
	"datascope-server/internal/management"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"slices"
	"strconv"
)

func businessMiddleware(server *mcp.Server, h handlers) {
	if h.opts.Management == nil {
		return
	}
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if h.opts.Catalog == nil {
				return nil, management.ErrDenied
			}
			d, err := h.opts.Catalog.Get(h.opts.DatabaseID)
			if err != nil {
				return nil, err
			}
			a, trusted := guard.Business(ctx)
			if !trusted {
				if !d.LocalAuthentication() {
					return nil, management.ErrDenied
				}
				if h.opts.AuthMode != "oauth" || req.GetExtra() == nil || req.GetExtra().TokenInfo == nil {
					return nil, management.ErrDenied
				}
				extra := req.GetExtra().TokenInfo.Extra
				if extra["identity"] != "database" || extra["database_id"] != d.ID {
					return nil, management.ErrDenied
				}
				username, ok := extra["username"].(string)
				if !ok {
					return nil, management.ErrDenied
				}
				a, err = h.opts.Catalog.BusinessAccount(ctx, d, username)
				if err != nil || strconv.FormatInt(a.ID, 10) != req.GetExtra().TokenInfo.UserID {
					return nil, management.ErrDenied
				}
			} else if a.DatabaseID != d.ID || !a.Active {
				return nil, management.ErrDenied
			}

			ctx = guard.WithBusiness(ctx, a)
			tool := method
			if method == "tools/call" {
				tool = req.(*mcp.CallToolRequest).Params.Name
			}
			ctx = audit.With(ctx, audit.Metadata{RequestID: audit.From(ctx).RequestID, DatabaseID: d.ID, SubjectID: a.BusinessUserID, Source: "business-mcp", Tool: tool, PolicyVersion: d.Revision})
			permission := "query"
			if tool == "list_actions" || tool == "describe_action" || tool == "invoke_action" {
				permission = "action"
			}
			if tool == "raw_query" || tool == "customer_scope" {
				permission = "raw_query"
			}
			if method == "tools/call" || method != "tools/list" && method != "initialize" && method != "notifications/initialized" && method != "ping" {
				if err = guard.Require(ctx, h.opts.Management, d.ID, permission); err != nil {
					return nil, err
				}
			}
			if tool == "search_knowledge" || tool == "read_knowledge" {
				if err = guard.Require(ctx, h.opts.Management, d.ID, "knowledge.read"); err != nil {
					return nil, err
				}
			}
			res, err := next(ctx, method, req)
			if err == nil && method == "tools/list" {
				list := res.(*mcp.ListToolsResult)
				allowed := []*mcp.Tool{}
				for _, t := range list.Tools {
					if t.Name == "list_actions" || t.Name == "describe_action" || t.Name == "invoke_action" {
						if !d.WriteEnabled || !slices.Contains(a.Permissions, "action") {
							continue
						}
					} else if !slices.Contains(a.Permissions, "query") {
						continue
					}
					if (t.Name == "raw_query" || t.Name == "customer_scope") && !slices.Contains(a.Permissions, "raw_query") {
						continue
					}
					if (t.Name == "search_knowledge" || t.Name == "read_knowledge") && !slices.Contains(a.Permissions, "knowledge.read") {
						continue
					}
					allowed = append(allowed, t)
				}
				list.Tools = allowed
			}
			return res, err
		}
	})
}

type knowledgeSearchInput struct {
	Query string `json:"query,omitempty"`
}
type knowledgeReadInput struct {
	ID string `json:"id"`
}

func (h handlers) searchKnowledge(ctx context.Context, _ *mcp.CallToolRequest, in knowledgeSearchInput) (*mcp.CallToolResult, any, error) {
	return h.knowledgeCall(ctx, "search_knowledge", application.Input{DatabaseID: h.opts.DatabaseID, Query: in.Query})
}
func (h handlers) readKnowledge(ctx context.Context, _ *mcp.CallToolRequest, in knowledgeReadInput) (*mcp.CallToolResult, any, error) {
	return h.knowledgeCall(ctx, "read_knowledge", application.Input{DatabaseID: h.opts.DatabaseID, ID: in.ID})
}
func (h handlers) knowledgeCall(ctx context.Context, action string, in application.Input) (*mcp.CallToolResult, any, error) {
	if h.opts.Application == nil {
		return nil, nil, fmt.Errorf("知识服务未配置")
	}
	out, e := h.opts.Application.Call(ctx, action, in)
	return nil, out, e
}
func (h handlers) businessID(ctx context.Context, req *mcp.CallToolRequest) (int64, error) {
	if a, ok := guard.Business(ctx); ok && a.DatabaseID == h.opts.DatabaseID {
		return a.BusinessUserID, nil
	}
	id, e := h.userID(req)
	if e != nil {
		return 0, e
	}
	if h.opts.Management != nil {
		return 0, management.ErrDenied
	}
	return id, nil
}

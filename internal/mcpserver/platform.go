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

func platformMiddleware(server *mcp.Server, h handlers) {
	if h.opts.Management == nil {
		return
	}
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			uid := h.opts.DefaultUserID
			if req.GetExtra() != nil && req.GetExtra().TokenInfo != nil {
				var e error
				uid, e = strconv.ParseInt(req.GetExtra().TokenInfo.UserID, 10, 64)
				if e != nil {
					return nil, e
				}
			} else if h.opts.AuthMode == "oauth" {
				return nil, management.ErrDenied
			} else if h.opts.AuthMode == "header" && req.GetExtra() != nil {
				if value := req.GetExtra().Header.Get(h.opts.UserHeader); value != "" {
					var e error
					uid, e = strconv.ParseInt(value, 10, 64)
					if e != nil {
						return nil, e
					}
				}
			}
			if h.opts.Catalog != nil {
				d, err := h.opts.Catalog.Get(h.opts.DatabaseID)
				if err != nil {
					return nil, err
				}
				if d.LocalAuthentication() {
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
					a, err := h.opts.Catalog.BusinessAccount(ctx, d, username)
					if err != nil || a.ID != uid {
						return nil, management.ErrDenied
					}
					ctx = guard.WithBusiness(ctx, a)
					tool := method
					if method == "tools/call" {
						tool = req.(*mcp.CallToolRequest).Params.Name
					}
					ctx = audit.With(ctx, audit.Metadata{RequestID: audit.From(ctx).RequestID, DatabaseID: d.ID, SubjectID: a.BusinessUserID, Source: "business-mcp", Tool: tool, PolicyVersion: d.Revision})
					permission := "query"
					if tool == "raw_query" || tool == "customer_scope" {
						permission = "raw_query"
					}
					if err = guard.Require(ctx, h.opts.Management, d.ID, permission); err != nil {
						return nil, err
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
			}
			u, e := h.opts.Management.User(ctx, uid)
			if e != nil || !u.Active {
				return nil, management.ErrDenied
			}
			ctx = guard.WithUser(ctx, u)
			audit.UpdateRequest(ctx, h.opts.DatabaseID, u.ID)
			tool := method
			if method == "tools/call" {
				tool = req.(*mcp.CallToolRequest).Params.Name
			}
			meta := audit.Metadata{RequestID: audit.From(ctx).RequestID, ActorID: u.ID, DatabaseID: h.opts.DatabaseID, Source: "database-mcp", Tool: tool}
			if h.svc != nil {
				meta.PolicyVersion = h.svc.PolicyVersion
			}
			ctx = audit.With(ctx, meta)
			p := "query"
			if tool == "raw_query" || tool == "customer_scope" {
				p = "raw_query"
			}
			if e := guard.Require(ctx, h.opts.Management, h.opts.DatabaseID, p); e != nil {
				ev, ae := h.opts.Management.Audit.Start(ctx, "access.denied", "")
				if ae == nil {
					ae = h.opts.Management.Audit.Finish(ev, e)
				}
				return nil, audit.Join(e, ae)
			}
			if method == "tools/call" && (tool == "search_knowledge" || tool == "read_knowledge") {
				if e := guard.Require(ctx, h.opts.Management, h.opts.DatabaseID, "knowledge.read"); e != nil {
					return nil, e
				}
			}
			res, e := next(ctx, method, req)
			if e == nil && method == "tools/list" {
				list := res.(*mcp.ListToolsResult)
				allowed := []*mcp.Tool{}
				for _, t := range list.Tools {
					if (t.Name == "raw_query" || t.Name == "customer_scope") && !h.opts.Management.Can(ctx, u.ID, h.opts.DatabaseID, "raw_query") {
						continue
					}
					if (t.Name == "search_knowledge" || t.Name == "read_knowledge") && !h.opts.Management.Can(ctx, u.ID, h.opts.DatabaseID, "knowledge.read") {
						continue
					}
					allowed = append(allowed, t)
				}
				list.Tools = allowed
			}
			return res, e
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
		return h.opts.Management.BusinessID(ctx, id, h.opts.DatabaseID)
	}
	return id, nil
}

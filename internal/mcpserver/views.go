package mcpserver

import (
	"context"
	"datascope-server/internal/guard"
	"datascope-server/internal/view"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

func documentationURI(database string) string {
	return "datascope://database/" + url.PathEscape(database) + "/documentation"
}

func (h handlers) viewResources(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
	d, err := h.opts.Catalog.Get(h.opts.DatabaseID)
	if err != nil {
		return nil, err
	}
	a, _ := guard.Business(ctx)
	canView := slices.Contains(a.Permissions, "query") || slices.Contains(a.Permissions, "action")
	if !d.MCPEnabled {
		return nil, &jsonrpc.Error{Code: -32002, Message: "数据库资源不可访问"}
	}
	if method == "resources/templates/list" {
		return &mcp.ListResourceTemplatesResult{ResourceTemplates: []*mcp.ResourceTemplate{}}, nil
	}
	if method == "resources/list" {
		items := []*mcp.Resource{}
		if d.Documentation != nil && *d.Documentation != "" {
			description := ""
			if d.Description != nil {
				description = *d.Description
			}
			items = append(items, &mcp.Resource{URI: documentationURI(d.ID), Name: "database-documentation", Title: d.Name + " · 使用说明", Description: description, MIMEType: "text/markdown", Size: int64(len(*d.Documentation)), Meta: mcp.Meta{"type": "database-documentation"}})
		}
		if canView && d.ViewsAvailable() {
			for _, v := range d.Views {
				if v.Published {
					items = append(items, &mcp.Resource{URI: view.URI(d.ID, v.ID), Name: v.ID, Title: v.Name, Description: v.Description, MIMEType: view.MIMEType, Size: int64(len(v.HTML)), Meta: mcp.Meta{"type": "view"}})
				}
			}
		}
		start := 0
		request := req.(*mcp.ListResourcesRequest)
		if request.Params != nil && request.Params.Cursor != "" {
			start, err = strconv.Atoi(request.Params.Cursor)
			if err != nil || start < 0 || start >= len(items) {
				return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "无效视图分页游标"}
			}
		}
		end := min(start+50, len(items))
		out := &mcp.ListResourcesResult{Resources: items[start:end], Cacheable: mcp.Cacheable{CacheScope: "private"}}
		if end < len(items) {
			out.NextCursor = strconv.Itoa(end)
		}
		return out, nil
	}
	request := req.(*mcp.ReadResourceRequest)
	if request.Params != nil && request.Params.URI == documentationURI(d.ID) && d.Documentation != nil && *d.Documentation != "" {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: request.Params.URI, MIMEType: "text/markdown", Text: *d.Documentation, Meta: mcp.Meta{"type": "database-documentation"}}}, Cacheable: mcp.Cacheable{CacheScope: "private"}}, nil
	}
	if !canView || !d.ViewsAvailable() {
		return nil, &jsonrpc.Error{Code: -32002, Message: "资源不存在或不可访问"}
	}
	prefix := view.URI(d.ID, "")
	if request.Params == nil || !strings.HasPrefix(request.Params.URI, prefix) {
		return nil, &jsonrpc.Error{Code: -32002, Message: "视图资源不存在"}
	}
	v, err := h.opts.Application.ReadView(ctx, d.ID, strings.TrimPrefix(request.Params.URI, prefix))
	if err != nil {
		return nil, &jsonrpc.Error{Code: -32002, Message: "视图资源不存在或不可访问"}
	}
	return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: request.Params.URI, MIMEType: view.MIMEType, Text: v.HTML, Meta: mcp.Meta{"type": "view"}}}, Cacheable: mcp.Cacheable{CacheScope: "private"}}, nil
}

func viewResult(database, id string) *mcp.CallToolResult {
	if id == "" {
		return nil
	}
	return &mcp.CallToolResult{Meta: mcp.Meta{"type": "view", "datascope/viewUri": view.URI(database, id)}}
}

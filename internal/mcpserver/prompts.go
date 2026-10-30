package mcpserver

import (
	"context"
	"datascope-server/internal/application"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (h handlers) knowledgeAvailable() bool {
	if h.opts.Catalog == nil {
		return false
	}
	d, err := h.opts.Catalog.Get(h.opts.DatabaseID)
	return err == nil && d.MCPEnabled && d.KnowledgeAvailable()
}
func (h handlers) knowledgePrompts(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
	if method == "prompts/list" {
		if !h.knowledgeAvailable() {
			return &mcp.ListPromptsResult{Prompts: []*mcp.Prompt{}, Cacheable: mcp.Cacheable{CacheScope: "private"}}, nil
		}
		request := req.(*mcp.ListPromptsRequest)
		cursor := ""
		if request.Params != nil {
			cursor = request.Params.Cursor
		}
		value, err := h.opts.Application.Call(ctx, "list_knowledge_prompts", application.Input{DatabaseID: h.opts.DatabaseID, Cursor: cursor})
		if err != nil {
			if cursor != "" {
				return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: err.Error()}
			}
			return nil, err
		}
		list := value.(*application.KnowledgePrompts)
		out := &mcp.ListPromptsResult{Prompts: []*mcp.Prompt{}, NextCursor: list.NextCursor, Cacheable: mcp.Cacheable{CacheScope: "private"}}
		for _, k := range list.Prompts {
			out.Prompts = append(out.Prompts, &mcp.Prompt{Name: k.Name, Title: k.Title, Description: k.Description})
		}
		return out, nil
	}
	if !h.knowledgeAvailable() {
		return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "该数据库未开启知识 Prompts"}
	}
	request := req.(*mcp.GetPromptRequest)
	if request.Params == nil || len(request.Params.Arguments) != 0 {
		return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "知识 Prompt 不接受模板参数"}
	}
	value, err := h.opts.Application.Call(ctx, "get_knowledge_prompt", application.Input{DatabaseID: h.opts.DatabaseID, Name: request.Params.Name})
	if err != nil {
		return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: err.Error()}
	}
	prompt := value.(*application.KnowledgePromptResult)
	return &mcp.GetPromptResult{Description: prompt.Description, Messages: []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: prompt.Messages[0].Content.Text}}}}, nil
}

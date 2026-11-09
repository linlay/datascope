package mcpserver

import (
	"context"
	"datascope-server/internal/application"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type searchKnowledgeInput struct {
	Keyword string `json:"keyword,omitempty" jsonschema:"可选关键词；缺省或空白时分页返回全部可读的已发布知识"`
	Cursor  string `json:"cursor,omitempty" jsonschema:"上一页的 next_cursor；翻页时保持 keyword 不变"`
}

type readKnowledgeInput struct {
	ID string `json:"id" jsonschema:"选中条目的精确知识 ID"`
}

func (h handlers) searchKnowledge(ctx context.Context, _ *mcp.CallToolRequest, in searchKnowledgeInput) (*mcp.CallToolResult, *application.KnowledgeSearch, error) {
	if h.opts.Application == nil {
		return nil, nil, fmt.Errorf("知识服务未配置")
	}
	out, err := h.opts.Application.Call(ctx, "search_knowledge", application.Input{DatabaseID: h.opts.DatabaseID, Keyword: in.Keyword, Cursor: in.Cursor})
	if err != nil {
		return nil, nil, err
	}
	return nil, out.(*application.KnowledgeSearch), nil
}

func (h handlers) readKnowledge(ctx context.Context, _ *mcp.CallToolRequest, in readKnowledgeInput) (*mcp.CallToolResult, application.Knowledge, error) {
	if h.opts.Application == nil {
		return nil, application.Knowledge{}, fmt.Errorf("知识服务未配置")
	}
	out, err := h.opts.Application.Call(ctx, "read_knowledge", application.Input{DatabaseID: h.opts.DatabaseID, ID: in.ID})
	if err != nil {
		return nil, application.Knowledge{}, err
	}
	return nil, out.(application.Knowledge), nil
}

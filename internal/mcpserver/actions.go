package mcpserver

import (
	"context"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type describeActionInput struct {
	Name string `json:"name"`
}

func (h handlers) listActions(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	if h.opts.Application == nil {
		return nil, nil, fmt.Errorf("action 服务未配置")
	}
	out, e := h.opts.Application.ListActions(ctx, h.opts.DatabaseID)
	return nil, out, e
}

type invokeActionInput struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
	RequestID string         `json:"request_id"`
}

func (h handlers) describeAction(ctx context.Context, _ *mcp.CallToolRequest, in describeActionInput) (*mcp.CallToolResult, any, error) {
	if in.Name == "" {
		return nil, nil, fmt.Errorf("需要 action 名称，请先调用 list_actions")
	}
	if h.opts.Application == nil {
		return nil, nil, fmt.Errorf("action 服务未配置")
	}
	out, e := h.opts.Application.DescribeAction(ctx, h.opts.DatabaseID, in.Name)
	return nil, out, e
}
func (h handlers) invokeAction(ctx context.Context, _ *mcp.CallToolRequest, in invokeActionInput) (*mcp.CallToolResult, any, error) {
	if h.opts.Application == nil {
		return nil, nil, fmt.Errorf("action 服务未配置")
	}
	out, e := h.opts.Application.InvokeAction(ctx, h.opts.DatabaseID, in.Name, in.RequestID, in.Arguments)
	return nil, out, e
}

package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"math"
	"slices"
	"datascope-server/internal/catalog"
)

func registerBusinessTools(server *mcp.Server, h handlers, tools []catalog.BusinessTool) {
	for _, tool := range tools {
		t := tool
		properties := map[string]any{}
		required := []string{}
		for name, p := range t.Parameters {
			properties[name] = map[string]any{"type": p.Type, "description": p.Description}
			required = append(required, name)
		}
		slices.Sort(required)
		template := t
		server.AddTool(&mcp.Tool{Name: t.Name, Description: t.Description, InputSchema: map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}, Annotations: readOnly(t.Name)}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			t := template
			current := h.current(ctx)
			if definitions, ok := ctx.Value(businessContextKey{}).([]catalog.BusinessTool); ok {
				found := false
				for _, definition := range definitions {
					if definition.Name == t.Name {
						t = definition
						found = true
						break
					}
				}
				if !found {
					return nil, fmt.Errorf("该业务工具已停用")
				}
			}
			var args map[string]any
			dec := json.NewDecoder(bytes.NewReader(req.Params.Arguments))
			dec.UseNumber()
			if e := dec.Decode(&args); e != nil {
				return nil, e
			}
			if len(args) != len(t.Parameters) {
				return nil, fmt.Errorf("参数数量不匹配")
			}
			bindings := map[string]any{}
			for name, p := range t.Parameters {
				value, ok := args[name]
				if !ok {
					return nil, fmt.Errorf("缺少参数 %s", name)
				}
				valid := false
				switch p.Type {
				case "string":
					_, valid = value.(string)
				case "boolean":
					_, valid = value.(bool)
				case "integer":
					if n, ok := value.(json.Number); ok {
						v, e := n.Int64()
						value = v
						valid = e == nil
					}
				case "number":
					if n, ok := value.(json.Number); ok {
						v, e := n.Float64()
						value = v
						valid = e == nil && !math.IsInf(v, 0) && !math.IsNaN(v)
					}
				}
				if !valid {
					return nil, fmt.Errorf("参数 %s 类型应为 %s", name, p.Type)
				}
				bindings["arg_"+name] = value
			}
			uid, e := current.businessID(ctx, req)
			if e != nil {
				return nil, e
			}
			out, e := current.svc.QueryWithParameters(ctx, t.SQL, uid, 200, bindings)
			if e != nil {
				return nil, e
			}
			raw, e := json.Marshal(out.QueryResult)
			if e != nil {
				return nil, e
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}}, StructuredContent: out.QueryResult}, nil
		})
	}
}

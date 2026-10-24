package catalog

import (
	"fmt"
	"regexp"
	"slices"
	"datascope-server/internal/rewrite"
)

type ToolParameter struct {
	Type        string `json:"type" yaml:"type"`
	Description string `json:"description" yaml:"description"`
}
type BusinessTool struct {
	Name        string                   `json:"name" yaml:"name"`
	Description string                   `json:"description" yaml:"description"`
	SQL         string                   `json:"sql" yaml:"sql"`
	Parameters  map[string]ToolParameter `json:"parameters" yaml:"parameters"`
}

var parameterName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,63}$`)

func validateBusinessTools(d Database, rw *rewrite.Rewriter) error {
	seen := map[string]bool{}
	for _, t := range d.BusinessTools {
		if !validID.MatchString(t.Name) || slices.Contains(Tools, t.Name) || seen[t.Name] || t.Description == "" {
			return fmt.Errorf("业务工具名称无效、重复或缺少说明: %s", t.Name)
		}
		seen[t.Name] = true
		names := []string{}
		for name, p := range t.Parameters {
			if !parameterName.MatchString(name) || !slices.Contains([]string{"string", "integer", "number", "boolean"}, p.Type) {
				return fmt.Errorf("业务工具 %s 的参数无效", t.Name)
			}
			names = append(names, "arg_"+name)
		}
		res, e := rw.RewriteWithParameters(t.SQL, names)
		if e != nil {
			return fmt.Errorf("业务工具 %s: %w", t.Name, e)
		}
		for _, n := range names {
			if !slices.Contains(res.Params, n) {
				return fmt.Errorf("业务工具 %s 未使用参数 %s", t.Name, n)
			}
		}
	}
	return nil
}

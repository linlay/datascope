package action

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/dop251/goja"
	"github.com/dop251/goja/ast"
	"github.com/dop251/goja/parser"
	"github.com/dop251/goja/unistring"
)

type site struct {
	name, scope, method, sql string
	count                    int
}

// Replace each literal tag with a host function closing over its static SQL.
// db/my themselves never exist in the VM. Even a forged template array, eval,
// alias or Function constructor cannot introduce SQL text into the host.
func compile(source string) (*goja.Program, []site, error) {
	tree, e := parser.ParseFile(nil, "action.js", source, 0)
	if e != nil {
		return nil, nil, e
	}
	sites := []site{}
	visited := map[uintptr]bool{}
	var walk func(reflect.Value) error
	walk = func(v reflect.Value) error {
		if !v.IsValid() {
			return nil
		}
		if v.Kind() == reflect.Interface {
			if v.IsNil() {
				return nil
			}
			return walk(v.Elem())
		}
		if v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return nil
			}
			ptr := v.Pointer()
			if visited[ptr] {
				return nil
			}
			visited[ptr] = true
			if t, ok := v.Interface().(*ast.TemplateLiteral); ok && t.Tag != nil {
				if dot, ok := t.Tag.(*ast.DotExpression); ok {
					if id, ok := dot.Left.(*ast.Identifier); ok && (id.Name.String() == "db" || id.Name.String() == "my") {
						scope, method := id.Name.String(), dot.Identifier.Name.String()
						if method != "one" && method != "all" && !(scope == "db" && method == "exec") {
							return fmt.Errorf("不支持 %s.%s", scope, method)
						}
						var b strings.Builder
						for i, part := range t.Elements {
							if !part.Valid {
								return fmt.Errorf("SQL 模板转义无效")
							}
							b.WriteString(part.Parsed.String())
							if i < len(t.Expressions) {
								b.WriteString(":arg_action" + strconv.Itoa(i))
							}
						}
						s := site{name: "__datascope_sql_" + strconv.Itoa(len(sites)), scope: scope, method: method, sql: b.String(), count: len(t.Expressions)}
						sites = append(sites, s)
						t.Tag = &ast.Identifier{Name: unistring.NewFromString(s.name), Idx: t.Tag.Idx0()}
					}
				}
			}
			return walk(v.Elem())
		}
		switch v.Kind() {
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				if v.Field(i).CanInterface() {
					if e := walk(v.Field(i)); e != nil {
						return e
					}
				}
			}
		case reflect.Slice:
			for i := 0; i < v.Len(); i++ {
				if e := walk(v.Index(i)); e != nil {
					return e
				}
			}
		}
		return nil
	}
	if e = walk(reflect.ValueOf(tree)); e != nil {
		return nil, nil, e
	}
	program, e := goja.CompileAST(tree, true)
	return program, sites, e
}

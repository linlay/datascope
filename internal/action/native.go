package action

// The qiuer-compatible authoring path uses main, CommonJS and connection
// methods. Literal SQL is still lowered to private, immutable host call sites:
// no runtime string (including eval/Function/aliased methods) can introduce SQL.
import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"path"
	"reflect"
	"strconv"
	"strings"

	"datascope-server/internal/store"
	"github.com/dop251/goja"
	"github.com/dop251/goja/ast"
	"github.com/dop251/goja/parser"
	"github.com/dop251/goja/unistring"
	"github.com/evanw/esbuild/pkg/api"
)

func isCommonJS(source string) bool {
	_, err := parser.ParseFile(nil, "module.js", source, 0)
	return err == nil
}

func hasMain(source string) bool {
	tree, err := parser.ParseFile(nil, "action.js", source, 0)
	if err != nil {
		return false
	}
	for _, n := range tree.Body {
		if f, ok := n.(*ast.FunctionDeclaration); ok && f.Function.Name != nil && f.Function.Name.Name.String() == "main" {
			return true
		}
	}
	return false
}

func visitAST(tree any, visit func(any) error) error {
	seen := map[uintptr]bool{}
	var walk func(reflect.Value) error
	walk = func(v reflect.Value) error {
		if !v.IsValid() {
			return nil
		}
		switch v.Kind() {
		case reflect.Interface:
			if !v.IsNil() {
				return walk(v.Elem())
			}
		case reflect.Pointer:
			if v.IsNil() || seen[v.Pointer()] {
				return nil
			}
			seen[v.Pointer()] = true
			if err := visit(v.Interface()); err != nil {
				return err
			}
			return walk(v.Elem())
		case reflect.Struct:
			if v.Type().PkgPath() == "github.com/dop251/goja/ast" {
				for i := 0; i < v.NumField(); i++ {
					if err := walk(v.Field(i)); err != nil {
						return err
					}
				}
			}
		case reflect.Slice:
			for i := 0; i < v.Len(); i++ {
				if err := walk(v.Index(i)); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(reflect.ValueOf(tree))
}

func checkCommonJS(source string) error {
	tree, err := parser.ParseFile(nil, "action.js", source, 0)
	if err != nil {
		return fmt.Errorf("Action 使用 CommonJS require/exports: %w", err)
	}
	return visitAST(tree, func(n any) error {
		if call, ok := n.(*ast.CallExpression); ok {
			if id, ok := call.Callee.(*ast.Identifier); ok && id.Name.String() == "require" {
				if len(call.ArgumentList) != 1 {
					return fmt.Errorf("require 必须使用一个静态相对路径")
				}
				if _, ok := call.ArgumentList[0].(*ast.StringLiteral); !ok {
					return fmt.Errorf("require 必须使用一个静态相对路径")
				}
			}
		}
		return nil
	})
}

func compileNative(source string, libraries []Library, database string) (*goja.Program, []site, error) {
	modules := map[string]string{}
	for _, lib := range libraries {
		if !libraryName.MatchString(lib.Name) {
			return nil, nil, fmt.Errorf("公共函数库名称无效: %s", lib.Name)
		}
		name := "scripts/" + lib.Name + ".js"
		if _, exists := modules[name]; exists {
			return nil, nil, fmt.Errorf("公共函数库名称重复: %s", lib.Name)
		}
		modules[name] = lib.Script
	}
	raw, _ := json.Marshal([]any{"qiuer-action-v1", database, source, modules})
	key := sha256.Sum256(raw)
	moduleCache.Lock()
	if item := moduleCache.items[key]; item != nil {
		moduleCache.lru.MoveToFront(item)
		value := item.Value.(*compiledModule)
		moduleCache.Unlock()
		return value.program, value.sites, nil
	}
	moduleCache.Unlock()
	if err := checkCommonJS(source); err != nil {
		return nil, nil, err
	}
	plugin := api.Plugin{Name: "action-commonjs", Setup: func(build api.PluginBuild) {
		build.OnResolve(api.OnResolveOptions{Filter: ".*"}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
			if args.Kind != api.ResolveJSRequireCall || (!strings.HasPrefix(args.Path, "./") && !strings.HasPrefix(args.Path, "../")) {
				return api.OnResolveResult{}, fmt.Errorf("仅允许当前数据库公共库的相对 require: %s", args.Path)
			}
			from := args.Importer
			if from == "" || from == "<stdin>" {
				from = "scripts/__action_entry.js"
			}
			name := path.Clean(path.Join(path.Dir(from), args.Path))
			if !strings.HasSuffix(name, ".js") {
				name += ".js"
			}
			if _, exists := modules[name]; !strings.HasPrefix(name, "scripts/") || !exists {
				return api.OnResolveResult{}, fmt.Errorf("公共函数库不存在或路径越界: %s", args.Path)
			}
			return api.OnResolveResult{Path: name, Namespace: "action-commonjs"}, nil
		})
		build.OnLoad(api.OnLoadOptions{Filter: ".*", Namespace: "action-commonjs"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
			text := modules[args.Path]
			if err := checkCommonJS(text); err != nil {
				return api.OnLoadResult{}, err
			}
			return api.OnLoadResult{Contents: &text, Loader: api.LoaderJS}, nil
		})
	}}
	result := api.Build(api.BuildOptions{
		Stdin:  &api.StdinOptions{Contents: source + "\n;module.exports.__datascope_main = main;", Sourcefile: "scripts/__action_entry.js", Loader: api.LoaderJS},
		Bundle: true, Write: false, Format: api.FormatIIFE, GlobalName: "__datascope_action",
		Platform: api.PlatformNeutral, Target: api.ES2020, TreeShaking: api.TreeShakingFalse,
		LogLevel: api.LogLevelSilent, Plugins: []api.Plugin{plugin},
	})
	if len(result.Errors) > 0 {
		return nil, nil, fmt.Errorf("动作模块编译失败: %s", strings.Join(api.FormatMessages(result.Errors, api.FormatMessagesOptions{Kind: api.ErrorMessage}), "\n"))
	}
	code := string(result.OutputFiles[0].Contents) + "\nfunction main(params,context){return __datascope_action.__datascope_main(params,context);}"
	tree, err := parser.ParseFile(nil, "action.js", code, 0)
	if err != nil {
		return nil, nil, err
	}
	sites := []site{}
	connections := map[string]bool{}
	var isConnection func(ast.Expression) bool
	isConnection = func(expression ast.Expression) bool {
		if id, ok := expression.(*ast.Identifier); ok {
			return connections[id.Name.String()]
		}
		if call, ok := expression.(*ast.CallExpression); ok {
			if dot, ok := call.Callee.(*ast.DotExpression); ok && dot.Identifier.Name.String() == "session" {
				if id, ok := dot.Left.(*ast.Identifier); ok && id.Name.String() == "db" {
					return true
				}
			}
		}
		return false
	}
	for {
		before := len(connections)
		_ = visitAST(tree, func(n any) error {
			if binding, ok := n.(*ast.Binding); ok && isConnection(binding.Initializer) {
				if id, ok := binding.Target.(*ast.Identifier); ok {
					connections[id.Name.String()] = true
				}
			}
			return nil
		})
		if before == len(connections) {
			break
		}
	}
	err = visitAST(tree, func(n any) error {
		call, ok := n.(*ast.CallExpression)
		if !ok {
			return nil
		}
		dot, ok := call.Callee.(*ast.DotExpression)
		if !ok {
			return nil
		}
		method := dot.Identifier.Name.String()
		if method != "query" && method != "queryOne" && method != "update" {
			return nil
		}
		known := isConnection(dot.Left)
		if len(call.ArgumentList) < 1 || len(call.ArgumentList) > 2 {
			if !known {
				return nil // An ordinary business method, not a connection call.
			}
			return fmt.Errorf("%s 需要 SQL 字面量和可选绑定参数", method)
		}
		literal, ok := call.ArgumentList[0].(*ast.StringLiteral)
		if !ok {
			if !known {
				return nil
			}
			return fmt.Errorf("%s 的 SQL 必须是静态字符串字面量", method)
		}
		query := literal.Value.String()
		parameters, err := store.ActionSQLParameters(query, method == "update")
		if err != nil {
			if !known {
				return nil
			}
			return err
		}
		s := site{name: "__datascope_sql_" + strconv.Itoa(len(sites)), method: method, sql: query, native: true, parameters: parameters, optionalReceiver: !known}
		sites = append(sites, s)
		call.Callee = &ast.Identifier{Name: unistring.NewFromString(s.name), Idx: call.Callee.Idx0()}
		call.ArgumentList = append([]ast.Expression{dot.Left}, call.ArgumentList[1:]...)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	program, err := goja.CompileAST(tree, true)
	if err != nil {
		return nil, nil, err
	}
	value := &compiledModule{key: key, program: program, sites: sites, bytes: len(raw) + len(code)}
	moduleCache.Lock()
	defer moduleCache.Unlock()
	if moduleCache.items[key] == nil && value.bytes <= 8<<20 {
		moduleCache.items[key] = moduleCache.lru.PushFront(value)
		moduleCache.bytes += value.bytes
		for moduleCache.lru.Len() > 128 || moduleCache.bytes > 8<<20 {
			item := moduleCache.lru.Back()
			old := item.Value.(*compiledModule)
			delete(moduleCache.items, old.key)
			moduleCache.bytes -= old.bytes
			moduleCache.lru.Remove(item)
		}
	}
	return program, sites, nil
}

func (s site) arguments(vm *goja.Runtime, value goja.Value) ([]any, error) {
	var positional bool
	for _, name := range s.parameters {
		if name == "?" {
			positional = true
		}
	}
	if positional {
		for _, name := range s.parameters {
			if name != "?" {
				return nil, fmt.Errorf("不能混用位置和命名参数")
			}
		}
	}
	var object *goja.Object
	if !goja.IsUndefined(value) && !goja.IsNull(value) {
		object, _ = value.(*goja.Object)
		if object == nil || (positional && object.ClassName() != "Array") || (!positional && object.ClassName() != "Object" && !(len(s.parameters) == 0 && object.ClassName() == "Array")) {
			return nil, fmt.Errorf("绑定参数应为命名对象或位置数组")
		}
	}
	if positional && object != nil && object.Get("length").ToInteger() != int64(len(s.parameters)) {
		return nil, fmt.Errorf("位置参数数量不匹配")
	}
	args := []any{}
	seen := map[string]bool{}
	for i, bind := range s.parameters {
		if !positional && seen[bind] {
			continue
		}
		seen[bind] = true
		key := strings.TrimPrefix(bind, ":")
		if positional {
			key = strconv.Itoa(i)
		}
		if object == nil {
			return nil, fmt.Errorf("缺少 SQL 参数 %s", key)
		}
		v := object.Get(key)
		if v == nil || goja.IsUndefined(v) {
			return nil, fmt.Errorf("缺少 SQL 参数 %s", key)
		}
		if _, ok := v.(*goja.Object); ok {
			return nil, fmt.Errorf("SQL 参数 %s 只接受标量值", key)
		}
		x := v.Export()
		switch n := x.(type) {
		case nil, string, bool:
		case int64:
			if n > 9007199254740991 || n < -9007199254740991 {
				return nil, fmt.Errorf("SQL 整数超出 JS 安全范围，请用字符串")
			}
		case float64:
			if !finite(n) || n > 9007199254740991 || n < -9007199254740991 {
				return nil, fmt.Errorf("SQL 数值无效或超出安全范围")
			}
		default:
			return nil, fmt.Errorf("SQL 参数 %s 类型不支持", key)
		}
		if positional {
			args = append(args, x)
		} else {
			args = append(args, sql.Named(key, x))
		}
	}
	return args, nil
}

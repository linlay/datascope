package action

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"time"

	"datascope-server/internal/rewrite"
	"datascope-server/internal/store"
	"github.com/dop251/goja"
)

type Write struct {
	SQL   string `json:"sql"`
	Rows  int64  `json:"rows"`
	Binds []any  `json:"binds"`
}
type Preview struct {
	Result any     `json:"result"`
	Writes []Write `json:"writes"`
}

type Denial string

func (d Denial) Error() string { return string(d) }

var ErrOutcomeUnknown = errors.New("事务结果未知，请核对审计，禁止自动重试")

type Runner struct {
	Libraries    []Library
	DatabaseID   string
	BeforeCommit func(*Preview) error
	Store        *store.Store
	Rewriter     *rewrite.Rewriter
	Bindings     map[string]any
	UserID       int64
	Now          time.Time
	// Timeout can shorten the fixed production limit for tests.
	Timeout time.Duration
}

// Run owns one transaction. Preview executes the same script and always rolls
// back. Any SQL error/deny is sticky, even if JavaScript catches the exception.
func (r Runner) Run(ctx context.Context, d Definition, args map[string]any, preview bool, expected *Preview) (out *Preview, err error) {
	if r.UserID <= 0 || r.UserID > 9007199254740991 {
		return nil, errors.New("业务身份超出 JavaScript 安全整数范围")
	}
	args, e := d.Arguments(args)
	if e != nil {
		return nil, e
	}
	timeout := r.Timeout
	if timeout <= 0 || timeout > 5*time.Second {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	program, sites, e := compileWithLibraries(d.Script, r.Libraries, r.DatabaseID)
	if e != nil {
		return nil, e
	}
	for _, s := range sites {
		if e = s.validate(); e != nil {
			return nil, e
		}
	}
	tx, e := r.Store.BeginAction(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	vm := goja.New()
	vm.SetMaxCallStackSize(128)
	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		select {
		case <-ctx.Done():
			vm.Interrupt(ctx.Err())
		case <-stopped:
		}
	}()
	var fatal error
	native := hasMain(d.Script)
	initializing := native || len(r.Libraries) > 0
	abort := func(e error) goja.Value {
		if fatal == nil {
			fatal = e
		}
		panic(vm.NewGoError(e))
	}
	vm.Set("deny", func(c goja.FunctionCall) goja.Value { return abort(Denial(c.Argument(0).String())) })
	parseJSON, _ := goja.AssertFunction(vm.Get("JSON").ToObject(vm).Get("parse"))
	nativeValue := func(value any) goja.Value {
		raw, err := json.Marshal(value)
		if err != nil {
			return abort(err)
		}
		result, err := parseJSON(goja.Undefined(), vm.ToValue(string(raw)))
		if err != nil {
			return abort(err)
		}
		return result
	}
	var connection *goja.Object
	if native {
		connection = vm.NewObject()
		for _, name := range []string{"query", "queryOne", "update"} {
			_ = connection.Set(name, func(goja.FunctionCall) goja.Value {
				return abort(errors.New("SQL 必须直接使用 query/queryOne/update 和静态字符串，不能使用动态调用"))
			})
		}
		db := vm.NewObject()
		_ = db.Set("session", func(c goja.FunctionCall) goja.Value {
			id := r.DatabaseID
			if id == "" {
				id = "default"
			}
			if len(c.Arguments) > 1 {
				return abort(errors.New("db.session 只接受可选数据库 ID"))
			}
			if v := c.Argument(0); !goja.IsUndefined(v) && !goja.IsNull(v) {
				name, ok := v.Export().(string)
				if !ok || name != "" && name != id {
					return abort(errors.New("Action 当前只允许连接所属数据库，额外数据库尚未开放"))
				}
			}
			return connection
		})
		_ = vm.GlobalObject().DefineDataProperty("db", db, goja.FLAG_FALSE, goja.FLAG_FALSE, goja.FLAG_TRUE)
		freeze, _ := goja.AssertFunction(vm.Get("Object").ToObject(vm).Get("freeze"))
		_, _ = freeze(goja.Undefined(), db)
		_, _ = freeze(goja.Undefined(), connection)
		if _, e = vm.RunString(nativePrelude); e != nil {
			return nil, e
		}
	}
	out = &Preview{Writes: []Write{}}
	for _, s := range sites {
		site := s
		if e = vm.GlobalObject().DefineDataProperty(site.name, vm.ToValue(func(c goja.FunctionCall) goja.Value {
			if site.native && site.optionalReceiver && c.Argument(0) != connection && !goja.IsNull(c.Argument(0)) && !goja.IsUndefined(c.Argument(0)) {
				// Business methods retain normal JS receiver/argument semantics,
				// including calls made during module initialization.
				if fn, ok := goja.AssertFunction(c.Argument(0).ToObject(vm).Get(site.method)); ok {
					value, err := fn(c.Argument(0), append([]goja.Value{vm.ToValue(site.sql)}, c.Arguments[1:]...)...)
					if err != nil {
						panic(err)
					}
					return value
				}
			}
			if initializing {
				return abort(errors.New("模块初始化阶段不能执行 SQL，请在函数中执行"))
			}
			if fatal != nil {
				return abort(fatal)
			}
			if ctx.Err() != nil {
				return abort(ctx.Err())
			}
			if site.native {
				if c.Argument(0) != connection {
					return abort(errors.New("SQL 连接必须来自 db.session()"))
				}
				args, err := site.arguments(vm, c.Argument(1))
				if err != nil {
					return abort(err)
				}
				if fatal != nil {
					return abort(fatal)
				}
				if site.method == "update" {
					result, err := tx.Update(ctx, site.sql, args...)
					if err != nil {
						return abort(err)
					}
					out.Writes = append(out.Writes, Write{SQL: site.sql, Rows: result.RowsAffected, Binds: args})
					var lastID any = result.LastInsertID
					if result.LastInsertID > 9007199254740991 || result.LastInsertID < -9007199254740991 {
						lastID = strconv.FormatInt(result.LastInsertID, 10)
					}
					return nativeValue(map[string]any{"rowsAffected": result.RowsAffected, "lastInsertId": lastID})
				}
				rows, err := tx.Query(ctx, site.sql, 1000, args...)
				if err != nil {
					return abort(err)
				}
				if site.method == "queryOne" {
					if len(rows.Rows) == 0 {
						return goja.Null()
					}
					return nativeValue(rows.Rows[0])
				}
				return nativeValue(rows.Rows)
			}
			if len(c.Arguments) != site.count+1 {
				return abort(errors.New("SQL 只接受模板调用"))
			}
			binds := map[string]any{}
			args := []any{}
			for i := 0; i < site.count; i++ {
				if _, object := c.Arguments[i+1].(*goja.Object); object {
					return abort(errors.New("SQL 插值不接受对象或数组"))
				}
				v := c.Arguments[i+1].Export()
				switch x := v.(type) {
				case nil, string, bool:
				case int64:
					if x > 9007199254740991 || x < -9007199254740991 {
						return abort(errors.New("SQL 整数超出 JS 安全范围，请用字符串"))
					}
				case float64:
					if !finite(x) || math.Abs(x) > 9007199254740991 {
						return abort(errors.New("SQL 数值无效或超出安全范围"))
					}
				default:
					return abort(errors.New("SQL 插值只接受字符串、有限数值、布尔或 null"))
				}
				n := "arg_action" + strconv.Itoa(i)
				binds[n] = v
				args = append(args, sql.Named(n, v))
			}
			if site.method == "exec" {
				n, e := tx.Exec(ctx, site.sql, args...)
				if e != nil {
					return abort(e)
				}
				out.Writes = append(out.Writes, Write{SQL: site.sql, Rows: n, Binds: args})
				return vm.ToValue(n)
			}
			query := site.sql
			limit := 1000
			if site.scope == "my" {
				if r.Rewriter == nil {
					return abort(errors.New("缺少查询权限配置"))
				}
				names := []string{}
				for n := range binds {
					names = append(names, n)
				}
				res, e := r.Rewriter.RewriteWithParameters(query, names)
				if e != nil {
					return abort(e)
				}
				values := map[string]any{}
				for n, v := range r.Bindings {
					values[n] = v
				}
				for n, v := range binds {
					values[n] = v
				}
				named := func(names []string) []any {
					a := []any{}
					for _, n := range names {
						a = append(a, sql.Named(n, values[n]))
					}
					return a
				}
				for _, g := range res.Guards {
					rows, e := tx.Query(ctx, rewrite.GuardQuery(g.SQL), 1, named(g.Params)...)
					if e != nil {
						return abort(e)
					}
					ok := false
					if len(rows.Rows) == 1 {
						for _, v := range rows.Rows[0] {
							switch x := v.(type) {
							case int64:
								ok = x != 0
							case bool:
								ok = x
							case float64:
								ok = finite(x) && x != 0
							case string:
								n, err := strconv.ParseFloat(x, 64)
								ok = err == nil && finite(n) && n != 0
							}
						}
					}
					if !ok {
						return abort(fmt.Errorf("无权查询表 %s: %s", g.Table, g.Message))
					}
				}
				for _, n := range res.TableNames() {
					if p, ok := r.Rewriter.Config().Lookup(n); ok && p.MaxRows > 0 {
						limit = min(limit, p.MaxRows)
					}
				}
				query = res.RewrittenSQL
				args = named(res.Params)
			}
			rows, e := tx.Query(ctx, query, limit, args...)
			if e != nil {
				return abort(e)
			}
			if site.method == "one" {
				if len(rows.Rows) == 0 {
					return goja.Null()
				}
				return vm.ToValue(rows.Rows[0])
			}
			return vm.ToValue(rows.Rows)
		}), goja.FLAG_FALSE, goja.FLAG_FALSE, goja.FLAG_FALSE); e != nil {
			return nil, e
		}
	}
	// Capture JSON intrinsics before untrusted script can replace them.
	stringify, _ := goja.AssertFunction(vm.Get("JSON").ToObject(vm).Get("stringify"))
	raw, _ := json.Marshal(map[string]any{"args": args, "ctx": map[string]any{"userId": r.UserID, "databaseId": r.DatabaseID, "now": r.Now.UTC().Format(time.RFC3339Nano)}})
	setup := `(function(){const x=JSON.parse(` + strconv.Quote(string(raw)) + `);function freeze(v){if(v&&typeof v==='object'){Object.keys(v).forEach(k=>freeze(v[k]));Object.freeze(v)}return v}Object.defineProperty(globalThis,'__input',{value:freeze(x),writable:false,configurable:false});})();`
	if _, e = vm.RunString(setup); e != nil {
		return nil, e
	}
	if native {
		_ = vm.Set("$context", vm.Get("__input").ToObject(vm).Get("args"))
	}
	if _, e = vm.RunProgram(program); e != nil {
		return nil, e
	}
	initializing = false
	entry := "run"
	if native {
		entry = "main"
	}
	run, ok := goja.AssertFunction(vm.Get(entry))
	if !ok {
		return nil, errors.New("脚本必须定义 function main(params, context)")
	}
	input := vm.Get("__input").ToObject(vm)
	value, e := run(goja.Undefined(), input.Get("args"), input.Get("ctx"))
	if fatal != nil {
		return nil, fatal
	}
	if e != nil {
		return nil, e
	}
	if value.ExportType() == reflect.TypeFor[*goja.Promise]() {
		if !native {
			return nil, errors.New("run 必须同步返回 JSON 对象")
		}
		promise := value.Export().(*goja.Promise)
		switch promise.State() {
		case goja.PromiseStateFulfilled:
			value = promise.Result()
		case goja.PromiseStateRejected:
			return nil, fmt.Errorf("main 执行失败: %s", promise.Result().String())
		default:
			return nil, errors.New("main 返回的 Promise 未完成，宿主没有事件循环")
		}
	}
	if native && goja.IsUndefined(value) {
		value = goja.Null()
	}
	serialized, e := stringify(goja.Undefined(), value)
	if fatal != nil {
		return nil, fatal
	}
	if e != nil {
		return nil, e
	}
	text := serialized.String()
	if len(text) > 1048576 {
		return nil, errors.New("action 返回值超过 1 MiB")
	}
	if e = json.Unmarshal([]byte(text), &out.Result); e != nil {
		return nil, errors.New("action 返回值必须是 JSON")
	}
	if expected != nil {
		a, _ := json.Marshal(out)
		b, _ := json.Marshal(expected)
		if string(a) != string(b) {
			return nil, errors.New("执行结果或写入计划已变化，请使用新的 request_id 重新预览")
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if !preview && r.BeforeCommit != nil {
		if e = r.BeforeCommit(out); e != nil {
			return nil, e
		}
	}
	if preview {
		if e = tx.Rollback(); e != nil {
			return nil, e
		}
	} else {
		if e = tx.Commit(); e != nil {
			return nil, fmt.Errorf("%w: %v", ErrOutcomeUnknown, e)
		}
	}
	return out, nil
}

package datasource

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"strings"
	"time"

	"datascope-server/internal/audit"
	"datascope-server/internal/store"
	"github.com/dop251/goja"
)

// Start reserves a durable run and a cross-process task lock before returning.
// A browser disconnect cannot abandon a running import.
func (m *Manager) Start(ctx context.Context, owner, id string, actor int64, trial, scheduled bool, parameters map[string]any) (*Run, error) {
	db, err := m.open(ctx, owner)
	if err != nil {
		return nil, err
	}
	lock, err := m.lock(owner, id)
	if err != nil {
		db.Close()
		return nil, err
	}
	release := func() { unlock(lock); db.Close() }
	var raw string
	var task Task
	if err = db.QueryRowContext(ctx, "SELECT DATA_ FROM IMPORT_TASK WHERE ID_=?", id).Scan(&raw); err != nil {
		release()
		return nil, err
	}
	if err = json.Unmarshal([]byte(raw), &task); err != nil {
		release()
		return nil, err
	}
	if scheduled {
		if !task.Enabled || task.NextRun == nil || task.NextRun.After(time.Now()) {
			release()
			return nil, errors.New("任务尚未到运行时间")
		}
		actor = task.Owner
	}
	if !m.Management.Can(ctx, actor, owner, "source.write") || !m.Management.Can(ctx, actor, owner, "config.read") {
		release()
		return nil, errors.New("任务执行者已无数据来源权限")
	}
	// Snapshot credentials and connection targets before accepting the run.
	connections := map[string]Connection{}
	secrets := map[string][]byte{}
	for _, id := range task.Sources {
		c, secret, e := loadConnection(ctx, db, id)
		if e != nil {
			release()
			return nil, e
		}
		connections[id] = c
		secrets[id] = secret
	}
	if parameters == nil {
		parameters = task.Parameters
	}
	if parameters == nil {
		parameters = map[string]any{}
	}
	if len(encode(parameters)) > 64<<10 {
		release()
		return nil, errors.New("参数最多 64 KiB")
	}
	trigger := "manual"
	if trial {
		trigger = "trial"
	}
	if scheduled {
		trigger = "schedule"
	}
	run := Run{ID: audit.ID(), TaskID: id, TaskName: task.Name, Revision: task.Revision, Actor: actor, Trigger: trigger, Status: "running", Started: time.Now().UTC(), Logs: []string{}, Parameters: parameters, Checkpoint: task.Checkpoint}
	state, err := db.BeginTx(ctx, nil)
	if err != nil {
		release()
		return nil, err
	}
	defer state.Rollback()
	if scheduled {
		task.NextRun, err = task.Schedule.Next(time.Now())
		if task.Schedule.Mode == "once" {
			task.Enabled = false
			task.NextRun = nil
		}
		if err == nil {
			var result sql.Result
			result, err = state.ExecContext(ctx, "UPDATE IMPORT_TASK SET DATA_=? WHERE ID_=? AND DATA_=?", encode(task), id, raw)
			if err == nil {
				n, e := result.RowsAffected()
				err = e
				if n != 1 {
					err = errors.New("任务计划已变化，请重试")
				}
			}
		}
	}
	if err == nil {
		_, err = state.ExecContext(ctx, "INSERT INTO IMPORT_RUN(ID_,TASK_ID_,DATA_,SCRIPT_) VALUES(?,?,?,?)", run.ID, id, encode(run), task.Script)
	}
	if err == nil {
		err = state.Commit()
	}
	if err != nil {
		release()
		return nil, err
	}
	// The returned value must not alias the goroutine's mutable progress.
	result := run
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		run.Status = "interrupted"
		run.Error = "服务正在关闭"
		_, _ = db.Exec("UPDATE IMPORT_RUN SET DATA_=? WHERE ID_=?", encode(run), run.ID)
		release()
		return nil, errors.New(run.Error)
	}
	execution, cancel := context.WithTimeout(context.Background(), time.Duration(task.Timeout)*time.Second)
	m.active[run.ID] = cancel
	m.wg.Add(1)
	m.mu.Unlock()
	go func() {
		defer func() {
			cancel()
			release()
			m.mu.Lock()
			delete(m.active, run.ID)
			m.mu.Unlock()
			m.wg.Done()
		}()
		meta := audit.Metadata{DatabaseID: owner, ActorID: actor, Source: "data-source", RequestID: run.ID, Tool: task.ID}
		execution = audit.With(execution, meta)
		done := make(chan struct{})
		watchDone := make(chan struct{})
		go func() {
			defer close(watchDone)
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-done:
					return
				case <-execution.Done():
					return
				case <-ticker.C:
					var stopped int
					if e := db.QueryRow("SELECT CANCEL_ FROM IMPORT_RUN WHERE ID_=?", run.ID).Scan(&stopped); e != nil || stopped != 0 || !m.Management.Can(context.Background(), actor, owner, "source.write") || !m.Management.Can(context.Background(), actor, owner, "config.read") {
						cancel()
						return
					}
				}
			}
		}()
		m.execute(execution, db, owner, task, connections, secrets, &run, trial)
		close(done)
		<-watchDone
	}()
	return &result, nil
}

func (m *Manager) Stop(ctx context.Context, owner, id string) error {
	db, err := m.open(ctx, owner)
	if err != nil {
		return err
	}
	defer db.Close()
	result, err := db.ExecContext(ctx, "UPDATE IMPORT_RUN SET CANCEL_=1 WHERE ID_=?", id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("运行记录不存在")
	}
	m.mu.Lock()
	if cancel := m.active[id]; cancel != nil {
		cancel()
	}
	m.mu.Unlock()
	return err
}

func (m *Manager) execute(ctx context.Context, state *sql.DB, owner string, task Task, connections map[string]Connection, secrets map[string][]byte, run *Run, trial bool) {
	var outcome error
	defer func() {
		if p := recover(); p != nil {
			outcome = fmt.Errorf("脚本执行失败: %v", p)
		}
		now := time.Now().UTC()
		run.Finished = &now
		if outcome != nil {
			if run.Status != "unknown" {
				run.Status = "failed"
				if ctx.Err() != nil {
					run.Status = "stopped"
				}
			}
			run.Error = outcome.Error()
			if len(run.Error) > 8192 {
				run.Error = run.Error[:8192]
			}
		}
		if _, err := state.Exec("UPDATE IMPORT_RUN SET DATA_=? WHERE ID_=?", encode(run), run.ID); err != nil {
			slog.Error("persist import outcome", "run", run.ID, "error", err)
		}
	}()
	vm := goja.New()
	vm.SetMaxCallStackSize(256)
	vmDone := make(chan struct{})
	defer close(vmDone)
	go func() {
		select {
		case <-ctx.Done():
			vm.Interrupt(ctx.Err())
		case <-vmDone:
		}
	}()
	opened := map[string]*store.Store{}
	readers := map[string]*store.ActionTx{}
	defer func() {
		for _, tx := range readers {
			tx.Rollback()
		}
		for _, st := range opened {
			st.Close()
		}
	}()
	fail := func(err error) {
		if outcome == nil {
			outcome = err
		}
		panic(vm.NewGoError(err))
	}
	budget := 0
	lastProgress := time.Now()
	progress := func() {
		if time.Since(lastProgress) > time.Second {
			if _, err := state.Exec("UPDATE IMPORT_RUN SET DATA_=? WHERE ID_=?", encode(run), run.ID); err != nil {
				fail(err)
			}
			lastProgress = time.Now()
		}
	}
	args := func(value goja.Value) []any {
		if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
			return nil
		}
		list, ok := value.Export().([]any)
		if !ok {
			fail(errors.New("SQL 参数必须为数组，使用 ? 占位符"))
		}
		for _, v := range list {
			switch v.(type) {
			case nil, string, bool, int64, int32, int, float64:
			default:
				fail(errors.New("SQL 参数只支持空值、字符串、布尔或数字"))
			}
			switch n := v.(type) {
			case float64:
				if math.IsNaN(n) || math.IsInf(n, 0) || math.Abs(n) > 9007199254740991 {
					fail(errors.New("数值参数超出安全范围，请使用字符串"))
				}
			case int64:
				if n > 9007199254740991 || n < -9007199254740991 {
					fail(errors.New("整数参数超出安全范围，请使用字符串"))
				}
			}
		}
		return list
	}
	query := func(tx *store.ActionTx, call goja.FunctionCall) goja.Value {
		budget++
		if budget > 100000 {
			fail(errors.New("导入最多执行 100000 条 SQL"))
		}
		result, err := tx.Query(ctx, call.Argument(0).String(), 1000, args(call.Argument(1))...)
		if err != nil {
			fail(err)
		}
		if result.Truncated {
			fail(errors.New("查询超过 1000 行，请在脚本中使用 LIMIT 和游标分批读取"))
		}
		run.Read += len(result.Rows)
		if run.Read > 1000000 {
			fail(errors.New("单次导入读取最多 100 万行，请使用进度分次执行"))
		}
		if len(encode(result.Rows)) > 8<<20 {
			fail(errors.New("单次查询结果最多 8 MiB，请减少读取批次或字段"))
		}
		progress()
		return vm.ToValue(result.Rows)
	}
	_ = vm.Set("sources", map[string]any{"get": func(call goja.FunctionCall) goja.Value {
		id := call.Argument(0).String()
		c, ok := connections[id]
		if !ok {
			fail(fmt.Errorf("任务未选择来源连接: %s", id))
		}
		if readers[id] == nil {
			st, err := m.source(ctx, owner, c, secrets[id], "")
			if err != nil {
				fail(err)
			}
			opened[id] = st
			readers[id], err = st.BeginImport(ctx, false)
			if err != nil {
				fail(err)
			}
		}
		return vm.ToValue(map[string]any{"query": func(call goja.FunctionCall) goja.Value { return query(readers[id], call) }})
	}})
	var target *store.ActionTx
	targetSession := func() goja.Value {
		if target == nil {
			d, err := m.Catalog.Get(owner)
			if err != nil {
				fail(err)
			}
			// SQLite has no separate credential. Network targets reuse the configured
			// writer and its explicit write-enable switch.
			var st *store.Store
			if d.Driver == "sqlite" {
				if err = m.Management.ValidatePath(d.Path); err != nil {
					fail(err)
				}
				st, err = store.OpenWriter(ctx, d.Path, 30*time.Second)
			} else {
				st, err = m.Catalog.OpenWriter(ctx, d)
			}
			if err != nil {
				fail(err)
			}
			opened["$target"] = st
			target, err = st.BeginImport(ctx, true)
			if err != nil {
				fail(err)
			}
		}
		return vm.ToValue(map[string]any{
			"query": func(call goja.FunctionCall) goja.Value { return query(target, call) },
			"update": func(call goja.FunctionCall) goja.Value {
				budget++
				if budget > 100000 {
					fail(errors.New("导入最多执行 100000 条 SQL"))
				}
				result, err := target.Update(ctx, call.Argument(0).String(), args(call.Argument(1))...)
				if err != nil {
					fail(err)
				}
				run.Written += result.RowsAffected
				progress()
				return vm.ToValue(map[string]any{"rowsAffected": result.RowsAffected, "lastInsertId": result.LastInsertID})
			},
		})
	}
	defer func() {
		if target != nil {
			target.Rollback()
		}
	}()
	_ = vm.Set("db", map[string]any{"target": targetSession})
	checkpoint := task.Checkpoint
	if checkpoint == nil {
		checkpoint = map[string]any{}
	}
	contextObject := map[string]any{"databaseId": owner, "runId": run.ID, "now": run.Started.Format(time.RFC3339), "trial": trial,
		"log": func(call goja.FunctionCall) goja.Value {
			if len(run.Logs) >= 200 {
				fail(errors.New("最多记录 200 条日志"))
			}
			parts := []string{}
			for _, v := range call.Arguments {
				parts = append(parts, v.String())
			}
			line := strings.Join(parts, " ")
			if len(line) > 2048 {
				line = line[:2048]
			}
			run.Logs = append(run.Logs, time.Now().UTC().Format(time.RFC3339)+" "+line)
			progress()
			return goja.Undefined()
		},
		"saveCheckpoint": func(value goja.Value) {
			var next map[string]any
			b, err := json.Marshal(value.Export())
			if err != nil || len(b) > 64<<10 {
				fail(errors.New("进度须为最多 64 KiB 的 JSON 对象"))
			}
			if err = json.Unmarshal(b, &next); err != nil || next == nil {
				fail(errors.New("进度须为 JSON 对象"))
			}
			checkpoint = next
		},
	}
	// JSON-backed arguments avoid exposing writable Go maps or host object types.
	setJSON := func(name string, v any) {
		text := encode(v)
		value, err := vm.RunString("JSON.parse(" + encode(text) + ")")
		if err != nil {
			fail(err)
		}
		_ = vm.Set(name, value)
	}
	setJSON("__parameters", run.Parameters)
	setJSON("__checkpoint", checkpoint)
	contextValue := vm.ToValue(contextObject).ToObject(vm)
	_ = contextValue.Set("checkpoint", vm.Get("__checkpoint"))
	_ = vm.Set("__context", contextValue)
	if _, err := vm.RunString(task.Script); err != nil {
		outcome = err
		return
	}
	main, ok := goja.AssertFunction(vm.Get("main"))
	if !ok {
		outcome = errors.New("脚本必须定义 function main(params, context)")
		return
	}
	result, err := main(goja.Undefined(), vm.Get("__parameters"), vm.Get("__context"))
	if err != nil {
		outcome = err
		return
	}
	if _, promise := result.Export().(*goja.Promise); promise {
		outcome = errors.New("导入脚本使用同步 main，不支持 Promise")
		return
	}
	if outcome != nil {
		return
	} // A caught SQL exception still poisons the run.
	if err = ctx.Err(); err != nil {
		outcome = err
		return
	}
	if trial {
		run.Status = "rolled_back"
		return
	}
	if !m.Management.Can(ctx, run.Actor, owner, "source.write") || !m.Management.Can(ctx, run.Actor, owner, "config.read") {
		outcome = errors.New("执行权限已撤销")
		return
	}
	if target != nil {
		if err = target.Commit(); err != nil {
			run.Status = "unknown"
			outcome = fmt.Errorf("目标提交结果未知，请核对数据后再重跑: %w", err)
			return
		}
	}
	// Target commit and management checkpoint cannot be a distributed transaction.
	// Only advance after commit. Scripts must tolerate replay of the last run.
	tx, err := state.Begin()
	if err != nil {
		run.Status = "unknown"
		outcome = err
		return
	}
	defer tx.Rollback()
	var raw string
	var current Task
	err = tx.QueryRow("SELECT DATA_ FROM IMPORT_TASK WHERE ID_=?", task.ID).Scan(&raw)
	if err == nil {
		err = json.Unmarshal([]byte(raw), &current)
	}
	if err == nil {
		current.Checkpoint = checkpoint
		_, err = tx.Exec("UPDATE IMPORT_TASK SET DATA_=? WHERE ID_=?", encode(current), task.ID)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		run.Status = "unknown"
		outcome = fmt.Errorf("目标已提交，但进度保存失败，请核对数据: %w", err)
		return
	}
	run.Checkpoint = checkpoint
	run.Status = "succeeded"
}

func (m *Manager) RunScheduler(ctx context.Context) {
	tick := func() {
		databases, err := m.Catalog.List()
		if err != nil {
			slog.Error("list import targets", "error", err)
			return
		}
		for _, d := range databases {
			// Do not create state for databases that have never used data sources.
			if _, err := os.Stat(m.directory(d.ID)); err != nil {
				continue
			}
			overview, err := m.List(ctx, d.ID)
			if err != nil {
				slog.Error("list import jobs", "database", d.ID, "error", err)
				continue
			}
			for _, t := range overview.Tasks {
				if t.Enabled && t.NextRun != nil && !t.NextRun.After(time.Now()) {
					if _, err = m.Start(ctx, d.ID, t.ID, t.Owner, false, true, nil); err != nil {
						slog.Debug("scheduled import not started", "database", d.ID, "task", t.ID, "error", err)
					}
				}
			}
		}
	}
	tick()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tick()
		}
	}
}

package ddl

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"datascope-server/internal/sqlitelock"
)

type Manager struct {
	DatabaseID, Path, StateDir string
	ActorID                    int64
	Revision                   int
	Validate                   func(context.Context) error
	BackupConfig               func(context.Context, string) error
	Discover                   func(context.Context) error
}

var stateID = regexp.MustCompile(`^[pe]_[a-f0-9]{32}$`)

func randomID(prefix string) string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return fmt.Sprintf("%s_%x", prefix, b)
}
func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func fileIdentity(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return fmt.Sprintf("%d:%d", stat.Dev, stat.Ino), nil
	}
	return path, nil
}

func (m Manager) prepare() error {
	for _, dir := range []string{m.StateDir, filepath.Join(m.StateDir, "plans"), filepath.Join(m.StateDir, "executions"), filepath.Join(m.StateDir, "backups")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	// Budget for a consistent backup plus a possible replacement table and journal.
	var bytes uint64
	for _, name := range []string{m.Path, m.Path + "-wal"} {
		if info, err := os.Stat(name); err == nil {
			bytes += uint64(info.Size())
		}
	}
	for _, dir := range []string{m.StateDir, filepath.Dir(m.Path)} {
		var stat syscall.Statfs_t
		if err := syscall.Statfs(dir, &stat); err != nil {
			return err
		}
		if uint64(stat.Bavail)*uint64(stat.Bsize) < bytes*3+(16<<20) {
			return errors.New("磁盘空间不足以完成 SQLite 备份与结构迁移")
		}
	}
	return nil
}
func writeJSON(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".pending-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
func (m Manager) read(kind, id string, out any) error {
	if !stateID.MatchString(id) {
		return errors.New("计划或执行 ID 无效")
	}
	raw, err := os.ReadFile(filepath.Join(m.StateDir, kind, id+".json"))
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}
func (m Manager) saveExecution(e *Execution) error {
	return writeJSON(filepath.Join(m.StateDir, "executions", e.ID+".json"), e)
}

func (m Manager) Preview(ctx context.Context, query string, changes []Change) (*Plan, error) {
	if (strings.TrimSpace(query) == "") == (len(changes) == 0) {
		return nil, errors.New("sql 与 changes 必须且只能提供一项")
	}
	if len(changes) > 100 {
		return nil, errors.New("一次最多 100 个变更")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if err := m.prepare(); err != nil {
		return nil, err
	}
	id := randomID("p")
	scratch := filepath.Join(m.StateDir, "."+id+".sqlite")
	defer func() {
		for _, s := range []string{"", "-wal", "-shm"} {
			_ = os.Remove(scratch + s)
		}
	}()
	unlock, err := sqlitelock.Acquire(ctx, m.Path)
	if err != nil {
		return nil, err
	}
	if m.Validate != nil {
		err = m.Validate(ctx)
	}
	identity, identityErr := fileIdentity(m.Path)
	if err == nil {
		err = identityErr
	}
	if err == nil {
		err = Snapshot(ctx, m.Path, scratch)
	}
	unlock()
	if err != nil {
		return nil, err
	}
	db, err := Open(ctx, scratch, true)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	before, err := Objects(ctx, conn)
	if err != nil {
		return nil, err
	}
	if _, err = conn.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		return nil, err
	}
	if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return nil, err
	}
	defer conn.ExecContext(context.Background(), "ROLLBACK")
	p := &Plan{ID: id, DatabaseID: m.DatabaseID, ActorID: m.ActorID, Revision: m.Revision, CreatedAt: now(), BeforeFingerprint: fingerprint(before), SQL: []string{}, Warnings: []string{}}
	p.FileIdentity = identity
	if query != "" {
		p.SQL, err = parseSQL(query)
		if err == nil {
			err = execute(ctx, conn, p.SQL)
		}
	} else {
		for _, c := range changes {
			var statements []string
			var rebuilt bool
			statements, rebuilt, err = compileChange(ctx, conn, c)
			if err != nil {
				break
			}
			p.RequiresRebuild = p.RequiresRebuild || rebuilt
			p.SQL = append(p.SQL, statements...)
			if err = execute(ctx, conn, statements); err != nil {
				break
			}
		}
	}
	if err != nil {
		return nil, fmt.Errorf("DDL 预检失败: %w", err)
	}
	if err = validateDatabase(ctx, conn); err != nil {
		return nil, err
	}
	after, err := Objects(ctx, conn)
	if err != nil {
		return nil, err
	}
	p.AfterFingerprint = fingerprint(after)
	p.Diff = difference(before, after)
	for _, d := range p.Diff {
		if d.Before != "" {
			p.Warnings = append(p.Warnings, "对象 "+d.ObjectType+" "+d.Name+" 将修改或删除；请核对关联的权限 SQL、Action 和查询")
		}
	}
	if p.RequiresRebuild {
		p.Warnings = append(p.Warnings, "需要复制整表数据并重建表；未映射的原字段数据将删除，类型亲和性转换按 SQLite 规则执行")
	}
	if len(p.Diff) == 0 {
		p.Warnings = append(p.Warnings, "本次没有结构差异")
	}
	if err = writeJSON(filepath.Join(m.StateDir, "plans", p.ID+".json"), p); err != nil {
		return nil, err
	}
	return p, nil
}

func execute(ctx context.Context, q querier, statements []string) error {
	for i, s := range statements {
		if _, err := q.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("第 %d 条语句: %w", i+1, err)
		}
	}
	return nil
}

func (m Manager) Apply(ctx context.Context, planID, requestID string) (out *Execution, err error) {
	if strings.TrimSpace(requestID) == "" || len(requestID) > 128 {
		return nil, errors.New("request_id 必填，最多 128 字符")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if err = m.prepare(); err != nil {
		return nil, err
	}
	unlock, err := sqlitelock.Acquire(ctx, m.Path)
	if err != nil {
		return nil, err
	}
	defer unlock()
	var p Plan
	if err = m.read("plans", planID, &p); err != nil {
		return nil, err
	}
	if p.DatabaseID != m.DatabaseID || p.ActorID != m.ActorID {
		return nil, errors.New("变更计划不属于当前数据库或操作用户")
	}
	hash := sha256.Sum256([]byte(fmt.Sprintf("%d:%s", m.ActorID, requestID)))
	id := fmt.Sprintf("e_%x", hash[:16])
	var previous Execution
	if e := m.read("executions", id, &previous); e == nil {
		if previous.PlanID != planID {
			return nil, errors.New("request_id 已用于其他计划")
		}
		m.recover(ctx, &previous)
		return &previous, nil
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	// A plan is consumable once, even if a retry supplies a new request ID.
	entries, e := m.executions()
	if e != nil {
		return nil, e
	}
	for _, old := range entries {
		if old.PlanID == planID {
			m.recover(ctx, &old)
			return &old, nil
		}
	}
	created, e := time.Parse(time.RFC3339Nano, p.CreatedAt)
	if e != nil || time.Since(created) > 30*time.Minute {
		return nil, errors.New("变更计划已过期，请重新预览")
	}
	if p.Revision != m.Revision {
		return nil, errors.New("数据库配置已更新，请重新预览")
	}
	identity, err := fileIdentity(m.Path)
	if err != nil {
		return nil, err
	}
	if identity != p.FileIdentity {
		return nil, errors.New("数据库文件已被替换，请重新预览")
	}
	if m.Validate != nil {
		if err = m.Validate(ctx); err != nil {
			return nil, err
		}
	}
	db, err := Open(ctx, m.Path, true)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	// foreign_keys must be changed outside the transaction on this dedicated connection.
	if _, err = conn.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		return nil, err
	}
	if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return nil, err
	}
	defer conn.ExecContext(context.Background(), "ROLLBACK")
	before, err := Objects(ctx, conn)
	if err != nil {
		return nil, err
	}
	if fingerprint(before) != p.BeforeFingerprint {
		return nil, errors.New("数据库结构已变化，请重新预览")
	}
	out = &Execution{ID: id, PlanID: p.ID, RequestID: requestID, DatabaseID: m.DatabaseID, ActorID: m.ActorID, Status: "running", StartedAt: now(), DiscoveryStatus: "not_started", SQL: p.SQL, Diff: p.Diff}
	if err = m.saveExecution(out); err != nil {
		return nil, err
	}
	commitAttempted := false
	defer func() {
		if err != nil {
			out.Status = "failed"
			if commitAttempted {
				out.Status = "interrupted"
			}
			out.Error = err.Error()
			out.FinishedAt = now()
			_ = m.saveExecution(out)
		}
	}()
	backupDir := filepath.Join(m.StateDir, "backups", id)
	if err = os.MkdirAll(backupDir, 0700); err != nil {
		return out, err
	}
	if m.BackupConfig != nil {
		if err = m.BackupConfig(ctx, filepath.Join(backupDir, "configuration")); err != nil {
			return out, err
		}
	}
	// The reserved write lock excludes external writers while the backup is taken.
	if err = Snapshot(ctx, m.Path, filepath.Join(backupDir, "database.sqlite")); err != nil {
		return out, err
	}
	out.BackupID = id
	if err = m.saveExecution(out); err != nil {
		return out, err
	}
	if m.Validate != nil {
		if err = m.Validate(ctx); err != nil {
			return out, err
		}
	}
	if err = execute(ctx, conn, p.SQL); err != nil {
		return out, err
	}
	if err = validateDatabase(ctx, conn); err != nil {
		return out, err
	}
	after, err := Objects(ctx, conn)
	if err != nil {
		return out, err
	}
	if fingerprint(after) != p.AfterFingerprint {
		return out, errors.New("执行后的结构与预览不一致，已回滚")
	}
	commitAttempted = true
	if _, err = conn.ExecContext(ctx, "COMMIT"); err != nil {
		return out, err
	}
	out.Status = "succeeded"
	out.FinishedAt = now()
	out.DiscoveryStatus = "pending"
	// Never report a post-commit bookkeeping failure as a rolled-back DDL.
	if e = m.saveExecution(out); e != nil {
		out.Error = "结构已提交，执行记录保存失败: " + e.Error()
		return out, nil
	}
	if m.Discover != nil {
		scanCtx, stop := context.WithTimeout(context.Background(), time.Minute)
		defer stop()
		if e = m.Discover(scanCtx); e != nil {
			out.DiscoveryStatus = "failed"
			out.Error = "结构已提交，侦查失败: " + e.Error()
		} else {
			out.DiscoveryStatus = "review_required"
		}
	}
	if e = m.saveExecution(out); e != nil {
		out.Error = "结构已提交，执行记录保存失败: " + e.Error()
	}
	return out, nil
}

func (m Manager) executions() ([]Execution, error) {
	entries, err := os.ReadDir(filepath.Join(m.StateDir, "executions"))
	if os.IsNotExist(err) {
		return []Execution{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Execution{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		var e Execution
		if err = m.read("executions", strings.TrimSuffix(entry.Name(), ".json"), &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt > out[j].StartedAt })
	return out, nil
}

// Called with the file lock held: an unfinished receipt belongs to an interrupted process.
func (m Manager) recover(ctx context.Context, e *Execution) {
	if e.Status != "running" && e.Status != "interrupted" {
		return
	}
	var p Plan
	if err := m.read("plans", e.PlanID, &p); err != nil {
		return
	}
	db, err := Open(ctx, m.Path, false)
	if err != nil {
		return
	}
	defer db.Close()
	objects, err := Objects(ctx, db)
	if err != nil {
		return
	}
	fp := fingerprint(objects)
	e.Status = "interrupted"
	e.Error = "执行被中断；请检查当前结构后重新制定计划"
	e.FinishedAt = now()
	if fp == p.AfterFingerprint && p.AfterFingerprint != p.BeforeFingerprint {
		e.Status = "succeeded"
		e.DiscoveryStatus = "pending"
		e.Error = "结构已提交；执行记录经恢复，请重新检查结构变化"
	} else if fp == p.BeforeFingerprint && p.BeforeFingerprint != p.AfterFingerprint {
		e.Status = "failed"
		e.Error = "执行被中断，结构事务未提交"
	}
	_ = m.saveExecution(e)
}

func (m Manager) GetExecution(ctx context.Context, id string) (*Execution, error) {
	var e Execution
	if err := m.read("executions", id, &e); err != nil {
		return nil, err
	}
	if e.Status == "running" || e.Status == "interrupted" {
		lockCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		defer cancel()
		if unlock, err := sqlitelock.Acquire(lockCtx, m.Path); err == nil {
			defer unlock()
			if err = m.read("executions", id, &e); err != nil {
				return nil, err
			}
			m.recover(ctx, &e)
		}
	}
	return &e, nil
}

func (m Manager) ListExecutions(ctx context.Context, limit, offset int) (map[string]any, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		return nil, errors.New("offset 不能为负数")
	}
	all, err := m.executions()
	if err != nil {
		return nil, err
	}
	out := []Execution{}
	for i := min(offset, len(all)); i < min(offset+limit, len(all)); i++ {
		e, err := m.GetExecution(ctx, all[i].ID)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return map[string]any{"items": out, "total": len(all)}, nil
}

func (m Manager) ListObjects(ctx context.Context, typ, query string, limit, offset int) (map[string]any, error) {
	db, err := Open(ctx, m.Path, false)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	all, err := Objects(ctx, db)
	if err != nil {
		return nil, err
	}
	filtered := []Object{}
	for _, o := range all {
		if (typ == "" || typ == o.ObjectType) && (query == "" || strings.Contains(strings.ToLower(o.Name), strings.ToLower(query))) {
			filtered = append(filtered, o)
		}
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		return nil, errors.New("offset 不能为负数")
	}
	return map[string]any{"items": filtered[min(offset, len(filtered)):min(offset+limit, len(filtered))], "total": len(filtered)}, nil
}

func (m Manager) GetObject(ctx context.Context, typ, name string) (*Object, error) {
	db, err := Open(ctx, m.Path, false)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	all, err := Objects(ctx, db)
	if err != nil {
		return nil, err
	}
	o, err := findObject(all, typ, name)
	return &o, err
}

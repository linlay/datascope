package application

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"datascope-server/internal/ddl"
	"datascope-server/internal/guard"
)

func (s *Service) DDL(ctx context.Context, action string, in Input) (any, error) {
	if err := guard.Require(ctx, s.Management, in.DatabaseID, "config.read"); err != nil {
		return nil, err
	}
	write := action == "preview_ddl" || action == "apply_ddl"
	if write {
		if err := guard.Require(ctx, s.Management, in.DatabaseID, "ddl.write"); err != nil {
			return nil, err
		}
	}
	d, err := s.Catalog.Get(in.DatabaseID)
	if err != nil {
		return nil, err
	}
	if !s.Catalog.CanEditSchema(d) {
		return nil, errors.New("此数据库尚未打开 DDL 管理，或不是允许的本地 SQLite 业务库")
	}
	// Legacy sources may not have a baseline yet. Capture it before any DDL,
	// so the post-commit scan cannot silently adopt newly created tables.
	if action == "preview_ddl" && (d.Discovery == nil || d.Discovery.Version == 0) {
		d, err = s.Catalog.Discover(ctx, d.ID)
		if err != nil {
			return nil, err
		}
	}
	path, err := filepath.EvalSymlinks(d.Path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	source, err := s.Management.Source(ctx, d.ID)
	if err != nil {
		return nil, err
	}
	u, _ := guard.User(ctx)
	m := ddl.Manager{DatabaseID: d.ID, Path: path, StateDir: filepath.Join(filepath.Dir(source.Path), "ddl"), Revision: d.Revision, ActorID: u.ID, BackupConfig: source.Backup}
	m.Validate = func(check context.Context) error {
		if err := guard.Require(check, s.Management, d.ID, "config.read"); err != nil {
			return err
		}
		if write {
			if err := guard.Require(check, s.Management, d.ID, "ddl.write"); err != nil {
				return err
			}
		}
		current, err := s.Catalog.Get(d.ID)
		if err != nil {
			return err
		}
		if !s.Catalog.CanEditSchema(current) || current.Revision != d.Revision {
			return errors.New("DDL 开关或数据库配置已变化，请重新预览")
		}
		actual, err := os.Stat(current.Path)
		if err != nil {
			return err
		}
		if !os.SameFile(info, actual) {
			return errors.New("数据库文件已被替换，请重新预览")
		}
		return nil
	}
	m.Discover = func(scan context.Context) error { _, err := s.Catalog.Discover(scan, d.ID); return err }
	switch action {
	case "list_ddl_objects":
		return m.ListObjects(ctx, in.ObjectType, in.Query, in.Limit, in.Offset)
	case "get_ddl_object":
		return m.GetObject(ctx, in.ObjectType, in.Name)
	case "preview_ddl":
		return m.Preview(ctx, in.SQL, in.Changes)
	case "apply_ddl":
		return m.Apply(ctx, in.PlanID, in.RequestID)
	case "get_ddl_execution":
		return m.GetExecution(ctx, in.ExecutionID)
	case "list_ddl_executions":
		return m.ListExecutions(ctx, in.Limit, in.Offset)
	}
	return nil, errors.New("未知 DDL 动作")
}

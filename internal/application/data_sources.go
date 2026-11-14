package application

import (
	"context"
	"errors"

	"datascope-server/internal/datasource"
	"datascope-server/internal/guard"
)

func (s *Service) DataSourceManager() *datasource.Manager {
	s.sourceOnce.Do(func() { s.sources = datasource.New(s.Management, s.Catalog) })
	return s.sources
}
func (s *Service) DataSources(ctx context.Context, action string, in Input) (any, error) {
	if err := guard.Require(ctx, s.Management, in.DatabaseID, "config.read"); err != nil {
		return nil, err
	}
	if err := guard.Require(ctx, s.Management, in.DatabaseID, "source.write"); err != nil {
		return nil, err
	}
	actor, ok := guard.User(ctx)
	if !ok {
		return nil, errors.New("需要平台身份")
	}
	m := s.DataSourceManager()
	switch action {
	case "get_import_run":
		return m.GetRun(ctx, in.DatabaseID, in.ID)
	case "list_data_sources":
		return m.List(ctx, in.DatabaseID)
	case "save_source_connection":
		if in.SourceConnection == nil {
			return nil, errors.New("缺少外部连接配置")
		}
		return m.SaveConnection(ctx, in.DatabaseID, *in.SourceConnection, in.Password)
	case "inspect_source_connection":
		c := datasource.Connection{ID: in.ID}
		if in.SourceConnection != nil {
			c = *in.SourceConnection
		}
		return m.Inspect(ctx, in.DatabaseID, c, in.Password, in.Table)
	case "delete_source_connection":
		return nil, m.Delete(ctx, in.DatabaseID, in.ID, true)
	case "save_import_task":
		if in.ImportTask == nil {
			return nil, errors.New("缺少任务配置")
		}
		return m.SaveTask(ctx, in.DatabaseID, *in.ImportTask, actor.ID)
	case "delete_import_task":
		return nil, m.Delete(ctx, in.DatabaseID, in.ID, false)
	case "run_import_task", "trial_import_task":
		return m.Start(ctx, in.DatabaseID, in.ID, actor.ID, action == "trial_import_task", false, in.Parameters)
	case "stop_import_run":
		return nil, m.Stop(ctx, in.DatabaseID, in.ID)
	}
	return nil, errors.New("未知数据来源操作")
}

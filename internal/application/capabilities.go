package application

import (
	"context"
	"datascope-server/internal/guard"
	"datascope-server/internal/management"
	"fmt"
)

// ConfigureCapability changes only one group's switch, preserving its definitions.
func (s *Service) ConfigureCapability(ctx context.Context, in Input) (any, error) {
	permission := map[string]string{"actions": "action.write", "knowledge": "knowledge.write", "views": "view.write"}[in.Capability]
	if permission == "" || in.Enabled == nil || in.Revision <= 0 {
		return nil, fmt.Errorf("需要有效的 capability、enabled 和 revision")
	}
	if err := guard.Require(ctx, s.Management, in.DatabaseID, permission); err != nil {
		if in.Capability == "actions" {
			return nil, err
		}
		if err = guard.Require(ctx, s.Management, in.DatabaseID, "mcp.write"); err != nil {
			return nil, err
		}
	}
	d, err := s.Catalog.Get(in.DatabaseID)
	if err != nil {
		return nil, err
	}
	if d.Revision != in.Revision {
		return nil, fmt.Errorf("配置已变更，请刷新后重试")
	}
	if in.Capability != "actions" && (in.WritePassword != "" || in.Database != nil) {
		return nil, fmt.Errorf("此开关不接受写入连接")
	}
	switch in.Capability {
	case "actions":
		d.WriteEnabled = *in.Enabled
		d.SetActionTools(*in.Enabled)
		if in.Database != nil {
			if in.Database.ID != d.ID {
				return nil, management.ErrDenied
			}
			d.WriteConnection = in.Database.WriteConnection
		}
	case "knowledge":
		d.KnowledgeEnabled = in.Enabled
	case "views":
		d.ViewsEnabled = in.Enabled
	}
	if err = s.Catalog.SaveWithWritePassword(ctx, *d, in.WritePassword); err != nil {
		return nil, err
	}
	saved, err := s.Catalog.Get(d.ID)
	if err != nil {
		return nil, err
	}
	return s.DatabaseView(ctx, *saved), nil
}

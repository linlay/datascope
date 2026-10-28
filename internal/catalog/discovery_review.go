package catalog

import (
	"context"
	"crypto/rand"
	"datascope-server/internal/management"
	"datascope-server/internal/store"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"
)

type PendingDiscovery struct {
	ID          string              `json:"id" yaml:"id"`
	BaseVersion int                 `json:"base_version" yaml:"base_version"`
	DetectedAt  time.Time           `json:"detected_at" yaml:"detected_at"`
	Tables      []store.TableSchema `json:"tables" yaml:"tables"`
	Changes     []SchemaChange      `json:"changes" yaml:"changes"`
}

type AnnotationResolution struct {
	Table        string `json:"table"`
	Column       string `json:"column,omitempty"`
	FollowSource bool   `json:"follow_source"`
}

// A scan only proposes changes. Approved structure and access configuration
// remain unchanged until an administrator reviews the exact candidate.
func (c *Catalog) stageDiscovery(ctx context.Context, d *Database, tables []store.TableSchema) (*Database, error) {
	changes := schemaDiff(d.Discovery.Tables, tables, d.Discovery.Annotations)
	var pending *PendingDiscovery
	if len(changes) > 0 {
		if old := d.Discovery.Pending; old != nil && old.BaseVersion == d.Discovery.Version && reflect.DeepEqual(old.Tables, tables) {
			pending = old
		} else {
			raw := make([]byte, 12)
			if _, err := rand.Read(raw); err != nil {
				return nil, err
			}
			pending = &PendingDiscovery{ID: fmt.Sprintf("change_%x", raw), BaseVersion: d.Discovery.Version, DetectedAt: time.Now().UTC(), Tables: tables, Changes: changes}
		}
	}
	if c.Management != nil {
		err := c.Management.UpdateRecords(ctx, "discovery_pending", "database.discovery_detected", d.ID, func([]management.Record) ([]management.Record, error) {
			current, err := c.Get(d.ID)
			if err != nil {
				return nil, err
			}
			if current.Revision != d.Revision {
				return nil, errors.New("扫描期间配置已更新，请重新检查")
			}
			raw, err := json.Marshal(pending)
			return []management.Record{{Key: d.ID, Data: string(raw)}}, err
		})
		if err != nil {
			return nil, err
		}
		if err := c.recordDiscoveryCheck(ctx, d.ID, time.Now().UTC(), nil); err != nil {
			return nil, err
		}
	} else {
		err := c.updateContext(ctx, "database.discovery_detected", d.ID, func(items *[]Database) error {
			for i := range *items {
				x := &(*items)[i]
				if x.ID == d.ID {
					if x.Revision != d.Revision || x.ETag != d.ETag {
						return errors.New("扫描期间配置已更新，请重新检查")
					}
					x.Discovery.Pending = pending
					x.Discovery.CheckedAt = time.Now().UTC()
					x.Discovery.AttemptedAt = x.Discovery.CheckedAt
					return nil
				}
			}
			return errors.New("数据库不存在")
		})
		if err != nil {
			return nil, err
		}
	}
	return c.Get(d.ID)
}

func (c *Catalog) ApplyDiscovery(ctx context.Context, id, candidate string, revision int, resolutions []AnnotationResolution) (*Database, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	d, err := c.Get(id)
	if err != nil {
		return nil, err
	}
	if d.Revision != revision {
		return nil, errors.New("配置已更新，请重新读取对比")
	}
	if d.Discovery == nil || d.Discovery.Pending == nil || d.Discovery.Pending.ID != candidate {
		return nil, errors.New("待确认的结构已变化，请重新检查")
	}
	pending := d.Discovery.Pending
	if pending.BaseVersion != d.Discovery.Version {
		return nil, errors.New("对比基线已更新，请重新检查")
	}
	st, err := c.OpenStore(ctx, d)
	if err != nil {
		return nil, err
	}
	defer st.Close()
	live, err := captureSchema(ctx, st)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(live, pending.Tables) {
		return nil, errors.New("数据库在对比后再次发生变化，请重新检查后确认")
	}
	for _, resolution := range resolutions {
		found := false
		for _, change := range schemaDiff(d.Discovery.Tables, pending.Tables, d.Discovery.Annotations) {
			if change.Table == resolution.Table && change.Column == resolution.Column && change.Property == "comment" && change.Manual != nil {
				found = true
			}
		}
		if !found {
			return nil, errors.New("注释处理项不在本次对比中")
		}
	}
	err = c.updateContext(ctx, "database.discovery_applied", id, func(items *[]Database) error {
		for i := range *items {
			x := &(*items)[i]
			if x.ID != id {
				continue
			}
			if x.Revision != revision || x.ETag != d.ETag {
				return errors.New("配置已更新，请重新读取对比")
			}
			// Generate the report before clearing chosen overrides so it records
			// the old manual value as well as the source change the user reviewed.
			applyDiscovery(x, pending.Tables)
			for _, resolution := range resolutions {
				if resolution.FollowSource {
					a := x.Discovery.Annotations[resolution.Table]
					if resolution.Column == "" {
						a.Comment = nil
					} else {
						v := a.Columns[resolution.Column]
						v.Comment = nil
						a.Columns[resolution.Column] = v
					}
					x.Discovery.Annotations[resolution.Table] = a
				}
			}
			x.Discovery.Pending = nil
			x.Revision++
			return nil
		}
		return errors.New("数据库不存在")
	})
	if err != nil {
		return nil, err
	}
	if c.Management != nil {
		// An older candidate is also hidden by base_version after the approved
		// snapshot commits, even if clearing this operational record fails.
		err = c.Management.UpdateRecords(ctx, "discovery_pending", "database.discovery_reviewed", id, func(records []management.Record) ([]management.Record, error) {
			for _, r := range records {
				if r.Key == id {
					var current *PendingDiscovery
					if err := json.Unmarshal([]byte(r.Data), &current); err != nil {
						return nil, err
					}
					if current != nil && current.ID != candidate {
						return nil, nil
					}
				}
			}
			return []management.Record{{Key: id, Data: "null"}}, nil
		})
		if err != nil {
			return nil, err
		}
	}
	return c.Get(id)
}

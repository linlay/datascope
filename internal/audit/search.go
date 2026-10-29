package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

type Filter struct {
	DatabaseID   string
	PlatformOnly bool
	RequestID    string
	ActorID      *int64
	Kind         string
	Source       string
	Status       string
	Tool         string
	Query        string
	StartedFrom  string
	StartedTo    string
	Limit        int
	Offset       int
}

type Page struct {
	Items      []Event          `json:"items"`
	Total      int64            `json:"total"`
	Limit      int              `json:"limit"`
	Offset     int              `json:"offset"`
	Kinds      []string         `json:"kinds"`
	Sources    []string         `json:"sources"`
	Actors     []int64          `json:"actors"`
	ActorNames map[int64]string `json:"actor_names"`
}

func (f *Filter) validate() error {
	if f.PlatformOnly && f.DatabaseID != "" {
		return errors.New("平台日志不能同时指定数据库")
	}
	if f.ActorID != nil && *f.ActorID < 0 {
		return errors.New("操作用户编号不能为负数")
	}
	if f.Status != "" && !slices.Contains([]string{"success", "failed", "running", "unknown"}, f.Status) {
		return errors.New("无效的日志状态")
	}
	for _, v := range []string{f.RequestID, f.Kind, f.Source, f.Tool, f.Query} {
		if len(v) > 1000 {
			return errors.New("日志筛选值最多 1000 字节")
		}
	}
	var from, to time.Time
	var err error
	if f.StartedFrom != "" {
		from, err = time.Parse(time.RFC3339Nano, f.StartedFrom)
		if err != nil {
			return errors.New("开始时间必须为 RFC3339 格式")
		}
	}
	if f.StartedTo != "" {
		to, err = time.Parse(time.RFC3339Nano, f.StartedTo)
		if err != nil {
			return errors.New("结束时间必须为 RFC3339 格式")
		}
	}
	if !from.IsZero() && !to.IsZero() && from.After(to) {
		return errors.New("开始时间不能晚于结束时间")
	}
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 100
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	return nil
}

// Search filters on the server before counting and paginating. In particular,
// a stale running event is consistently classified as unknown in both results
// and status filters. All user-controlled values remain SQL parameters.
func (s *Store) Search(ctx context.Context, f Filter) (*Page, error) {
	return s.search(ctx, f, true)
}

func (s *Store) search(ctx context.Context, f Filter, facets bool) (*Page, error) {
	if err := f.validate(); err != nil {
		return nil, err
	}
	cutoff := time.Now().UTC().Add(-2 * time.Minute)
	where := []string{"1=1"}
	args := []any{}
	if f.DatabaseID != "" {
		where = append(where, "database_id=?")
		args = append(args, f.DatabaseID)
	}
	if f.PlatformOnly {
		where = append(where, "database_id=''")
	}
	scope := " WHERE " + strings.Join(where, " AND ")
	scopeArgs := append([]any(nil), args...)
	if f.RequestID != "" {
		where = append(where, "request_id=?")
		args = append(args, f.RequestID)
	}
	if f.ActorID != nil {
		where = append(where, "actor_id=?")
		args = append(args, *f.ActorID)
	}
	if f.Kind != "" {
		where = append(where, "kind=?")
		args = append(args, f.Kind)
	}
	for _, entry := range []struct{ field, value string }{{"source", f.Source}, {"tool", f.Tool}} {
		if entry.value != "" {
			where = append(where, "json_extract(data,'$."+entry.field+"')=?")
			args = append(args, entry.value)
		}
	}
	if f.Status != "" {
		where = append(where, `CASE WHEN json_extract(data,'$.status')='running' AND julianday(started_at)<julianday(?) THEN 'unknown' ELSE json_extract(data,'$.status') END=?`)
		args = append(args, cutoff.Format(time.RFC3339Nano), f.Status)
	}
	if f.StartedFrom != "" {
		where = append(where, "julianday(started_at)>=julianday(?)")
		args = append(args, f.StartedFrom)
	}
	if f.StartedTo != "" {
		where = append(where, "julianday(started_at)<=julianday(?)")
		args = append(args, f.StartedTo)
	}
	if f.Query != "" {
		where = append(where, "instr(lower(data),lower(?))>0")
		args = append(args, f.Query)
	}
	clause := " WHERE " + strings.Join(where, " AND ")
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	out := &Page{Items: []Event{}, Limit: f.Limit, Offset: f.Offset, Kinds: []string{}, Sources: []string{}, Actors: []int64{}, ActorNames: map[int64]string{}}
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_events"+clause, args...).Scan(&out.Total); err != nil {
		return nil, err
	}
	pageArgs := append(append([]any(nil), args...), f.Limit, f.Offset)
	rows, err := tx.QueryContext(ctx, "SELECT data FROM audit_events"+clause+" ORDER BY julianday(started_at) DESC,id DESC LIMIT ? OFFSET ?", pageArgs...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var raw string
		var event Event
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &event); err != nil {
			rows.Close()
			return nil, err
		}
		if event.Status == "running" {
			if started, e := time.Parse(time.RFC3339Nano, event.StartedAt); e == nil && started.Before(cutoff) {
				event.Status = "unknown"
			}
		}
		out.Items = append(out.Items, event)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if facets {
		for _, field := range []string{"kind", "json_extract(data,'$.source')", "actor_id"} {
			rows, err := tx.QueryContext(ctx, "SELECT DISTINCT "+field+" FROM audit_events"+scope+" ORDER BY 1 LIMIT 200", scopeArgs...)
			if err != nil {
				return nil, err
			}
			for rows.Next() {
				if field == "actor_id" {
					var actor int64
					if err = rows.Scan(&actor); err != nil {
						break
					}
					out.Actors = append(out.Actors, actor)
				} else {
					var value sql.NullString
					if err = rows.Scan(&value); err != nil {
						break
					}
					if value.String != "" {
						if field == "kind" {
							out.Kinds = append(out.Kinds, value.String)
						} else {
							out.Sources = append(out.Sources, value.String)
						}
					}
				}
			}
			if err == nil {
				err = rows.Err()
			}
			rows.Close()
			if err != nil {
				return nil, fmt.Errorf("读取日志筛选项: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

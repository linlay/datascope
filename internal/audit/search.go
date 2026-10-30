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
	Items []Event `json:"items"`

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

func (s *Store) Search(ctx context.Context, f Filter) (*Page, error) {
	return s.search(ctx, f, true)
}
func (s *Store) localSearch(ctx context.Context, f Filter, facets bool) (*Page, error) {
	s.Flush(ctx)
	if s.db == nil {
		return nil, fmt.Errorf("审计存储暂不可用")
	}
	if err := f.validate(); err != nil {
		return nil, err
	}
	cutoff := time.Now().UTC().Add(-2 * time.Minute)
	where := []string{"1=1"}
	args := []any{}
	if f.DatabaseID != "" {
		where = append(where, "DATABASE_ID_=?")
		args = append(args, f.DatabaseID)
	}
	if f.PlatformOnly {
		where = append(where, "DATABASE_ID_=''")
	}
	scope := " WHERE " + strings.Join(where, " AND ")
	scopeArgs := append([]any(nil), args...)
	if f.RequestID != "" {
		where = append(where, "REQUEST_ID_=?")
		args = append(args, f.RequestID)
	}
	if f.ActorID != nil {
		where = append(where, "ACTOR_ID_=?")
		args = append(args, *f.ActorID)
	}
	if f.Kind != "" {
		where = append(where, "KIND_=?")
		args = append(args, f.Kind)
	}
	for _, entry := range []struct{ field, value string }{{"source", f.Source}, {"tool", f.Tool}} {
		if entry.value != "" {
			where = append(where, "json_extract(DATA_,'$."+entry.field+"')=?")
			args = append(args, entry.value)
		}
	}
	if f.Status != "" {
		where = append(where, "CASE WHEN json_extract(DATA_,'$.status')='running' AND julianday(STARTED_AT_)<julianday(?) THEN 'unknown' ELSE json_extract(DATA_,'$.status') END=?")
		args = append(args, cutoff.Format(time.RFC3339Nano), f.Status)
	}
	if f.StartedFrom != "" {
		where = append(where, "julianday(STARTED_AT_)>=julianday(?)")
		args = append(args, f.StartedFrom)
	}
	if f.StartedTo != "" {
		where = append(where, "julianday(STARTED_AT_)<=julianday(?)")
		args = append(args, f.StartedTo)
	}
	if f.Query != "" {
		where = append(where, "instr(lower(DATA_),lower(?))>0")
		args = append(args, f.Query)
	}
	clause := " WHERE " + strings.Join(where, " AND ")
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	out := &Page{Items: []Event{}, Limit: f.Limit, Offset: f.Offset, Kinds: []string{}, Sources: []string{}, Actors: []int64{}, ActorNames: map[int64]string{}}
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM AUDIT_EVENT"+clause, args...).Scan(&out.Total); err != nil {
		return nil, err
	}
	pageArgs := append(append([]any(nil), args...), f.Limit, f.Offset)
	rows, err := tx.QueryContext(ctx, "SELECT DATA_ FROM AUDIT_EVENT"+clause+" ORDER BY julianday(STARTED_AT_) DESC,ID_ DESC LIMIT ? OFFSET ?", pageArgs...)
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
		for _, field := range []string{"KIND_", "json_extract(DATA_,'$.source')", "ACTOR_ID_"} {
			rows, err := tx.QueryContext(ctx, "SELECT DISTINCT "+field+" FROM AUDIT_EVENT"+scope+" ORDER BY 1 LIMIT 200", scopeArgs...)
			if err != nil {
				return nil, err
			}
			for rows.Next() {
				if field == "ACTOR_ID_" {
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
						if field == "KIND_" {
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

func (s *Store) search(ctx context.Context, f Filter, facets bool) (*Page, error) {
	if s == nil {
		return &Page{Items: []Event{}}, nil
	}
	if s.Source != nil && f.DatabaseID != "" {
		if target := s.Source(f.DatabaseID); target != nil && target != s {
			return target.localSearch(ctx, f, facets)
		}
	}
	if s.Sources == nil || f.PlatformOnly {
		return s.localSearch(ctx, f, facets)
	}
	if e := f.validate(); e != nil {
		return nil, e
	}
	all := []*Store{s}
	all = append(all, s.Sources()...)
	out := &Page{Items: []Event{}, Limit: f.Limit, Offset: f.Offset, ActorNames: map[int64]string{}}
	events := []Event{}
	for _, st := range all {
		local := f
		local.Offset = 0
		local.Limit = 200
		for {
			p, e := st.localSearch(ctx, local, facets)
			if e != nil {
				break
			}
			if local.Offset == 0 {
				out.Total += p.Total
				out.Kinds = append(out.Kinds, p.Kinds...)
				out.Sources = append(out.Sources, p.Sources...)
				out.Actors = append(out.Actors, p.Actors...)
			}
			events = append(events, p.Items...)
			if len(p.Items) < 200 || local.Offset+200 >= f.Offset+f.Limit {
				break
			}
			local.Offset += 200
		}
	}
	slices.SortFunc(events, func(a, b Event) int {
		if a.StartedAt != b.StartedAt {
			return strings.Compare(b.StartedAt, a.StartedAt)
		}
		return strings.Compare(b.ID, a.ID)
	})
	from := min(f.Offset, len(events))
	to := min(from+f.Limit, len(events))
	out.Items = events[from:to]
	slices.Sort(out.Kinds)
	out.Kinds = slices.Compact(out.Kinds)
	slices.Sort(out.Sources)
	out.Sources = slices.Compact(out.Sources)
	slices.Sort(out.Actors)
	out.Actors = slices.Compact(out.Actors)
	return out, nil
}

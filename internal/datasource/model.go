// Package datasource manages database-scoped, scripted imports independently of
// business Actions. All source access is read-only; only the owning target can write.
package datasource

import (
	"errors"
	"fmt"
	"regexp"
	"time"
	_ "time/tzdata"

	"datascope-server/internal/store"
	"github.com/dop251/goja"
)

var identifier = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,63}$`)

type Connection struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Driver      string            `json:"driver"`
	Path        string            `json:"path,omitempty"`
	Connection  *store.Connection `json:"connection,omitempty"`
	Revision    int               `json:"revision"`
	HasPassword bool              `json:"has_password"`
}
type Schedule struct {
	Mode     string `json:"mode"`
	At       string `json:"at,omitempty"`
	Minutes  int    `json:"minutes,omitempty"`
	Time     string `json:"time,omitempty"`
	Timezone string `json:"timezone"`
}
type Task struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Script      string         `json:"script"`
	Sources     []string       `json:"sources"`
	Parameters  map[string]any `json:"parameters"`
	Schedule    Schedule       `json:"schedule"`
	Enabled     bool           `json:"enabled"`
	Timeout     int            `json:"timeout"`
	Revision    int            `json:"revision"`
	Owner       int64          `json:"owner"`
	NextRun     *time.Time     `json:"next_run,omitempty"`
	Checkpoint  map[string]any `json:"checkpoint"`
	LastRun     *Run           `json:"last_run,omitempty"`
}
type Run struct {
	Script     string         `json:"script,omitempty"`
	ID         string         `json:"id"`
	TaskID     string         `json:"task_id"`
	TaskName   string         `json:"task_name"`
	Revision   int            `json:"revision"`
	Actor      int64          `json:"actor"`
	Trigger    string         `json:"trigger"`
	Status     string         `json:"status"`
	Started    time.Time      `json:"started"`
	Finished   *time.Time     `json:"finished,omitempty"`
	Read       int            `json:"read"`
	Written    int64          `json:"written"`
	Logs       []string       `json:"logs"`
	Error      string         `json:"error,omitempty"`
	Parameters map[string]any `json:"parameters"`
	Checkpoint map[string]any `json:"checkpoint"`
}
type Overview struct {
	Connections []Connection `json:"connections"`
	Tasks       []Task       `json:"tasks"`
	Runs        []Run        `json:"runs"`
}

func (s Schedule) Next(now time.Time) (*time.Time, error) {
	if s.Timezone == "" {
		s.Timezone = "Asia/Shanghai"
	}
	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return nil, errors.New("无效时区")
	}
	var next time.Time
	switch s.Mode {
	case "manual":
		return nil, nil
	case "once":
		next, err = time.Parse(time.RFC3339, s.At)
		if err != nil {
			return nil, errors.New("指定时间需要包含时区的 ISO 时间")
		}
	case "interval":
		if s.Minutes < 1 || s.Minutes > 525600 {
			return nil, errors.New("运行间隔须为 1–525600 分钟")
		}
		next = now.Add(time.Duration(s.Minutes) * time.Minute)
	case "daily":
		clock, e := time.Parse("15:04", s.Time)
		if e != nil {
			return nil, errors.New("每天运行时间使用 HH:mm")
		}
		local := now.In(loc)
		next = time.Date(local.Year(), local.Month(), local.Day(), clock.Hour(), clock.Minute(), 0, 0, loc)
		if !next.After(now) {
			next = next.AddDate(0, 0, 1)
		}
	default:
		return nil, errors.New("不支持的运行计划")
	}
	next = next.UTC()
	return &next, nil
}

func (t *Task) validate(now time.Time) error {
	if !identifier.MatchString(t.ID) || len(t.Name) == 0 || len(t.Name) > 200 {
		return errors.New("任务 ID 或名称无效")
	}
	if len(t.Script) == 0 || len(t.Script) > 256<<10 {
		return errors.New("脚本必填，最多 256 KiB")
	}
	if _, err := goja.Compile(t.ID+".js", t.Script, false); err != nil {
		return fmt.Errorf("脚本语法错误: %w", err)
	}
	if len(t.Sources) > 16 {
		return errors.New("最多选择 16 个来源连接")
	}
	if t.Timeout == 0 {
		t.Timeout = 300
	}
	if t.Timeout < 1 || t.Timeout > 3600 {
		return errors.New("执行时限须为 1–3600 秒")
	}
	if t.Schedule.Timezone == "" {
		t.Schedule.Timezone = "Asia/Shanghai"
	}
	next, err := t.Schedule.Next(now)
	if err != nil {
		return err
	}
	if t.Enabled && next == nil {
		return errors.New("仅手动任务不能开启自动运行")
	}
	if t.Enabled && t.Schedule.Mode == "once" && !next.After(now) {
		return errors.New("指定运行时间须在未来")
	}
	t.NextRun = nil
	if t.Enabled {
		t.NextRun = next
	}
	return nil
}

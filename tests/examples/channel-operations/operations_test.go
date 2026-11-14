package operations_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"datascope-server/internal/action"
	"datascope-server/internal/configbundle"
	"datascope-server/internal/rewrite"
	"datascope-server/internal/service"
	"datascope-server/internal/store"
)

func generate(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "operations.sqlite3")
	if out, e := exec.Command("python3", "../../../examples/channel-operations/generate.py", "--db", p).CombinedOutput(); e != nil {
		t.Fatalf("generate: %s %v", out, e)
	}
	return p
}
func open(t *testing.T, path string) (*store.Store, *service.QueryService, *rewrite.Rewriter, *configbundle.Bundle) {
	t.Helper()
	ctx := context.Background()
	st, e := store.OpenWriter(ctx, path, 20*time.Second)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { st.Close() })
	b, e := configbundle.Load("../../../examples/channel-operations/bundle.yml")
	if e != nil {
		t.Fatal(e)
	}
	svc, e := service.New(ctx, st, b.Database.Config, time.Date(2026, 11, 13, 4, 0, 0, 0, time.UTC))
	if e != nil {
		t.Fatal(e)
	}
	names, e := st.TableNames(ctx)
	if e != nil {
		t.Fatal(e)
	}
	rw, e := rewrite.New(b.Database.Config, names)
	if e != nil {
		t.Fatal(e)
	}
	return st, svc, rw, b
}
func raw(t *testing.T, st *store.Store, q string) map[string]any {
	t.Helper()
	r, e := st.RawQuery(context.Background(), q, 1)
	if e != nil || r.RowCount != 1 {
		t.Fatalf("%s: %v %v", q, r, e)
	}
	return r.Rows[0]
}
func count(t *testing.T, st *store.Store, q string) int64 {
	t.Helper()
	return raw(t, st, q)["n"].(int64)
}
func copyDB(t *testing.T, source string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "case.sqlite3")
	a, e := os.Open(source)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	b, e := os.Create(p)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = io.Copy(b, a); e != nil {
		t.Fatal(e)
	}
	if e = b.Close(); e != nil {
		t.Fatal(e)
	}
	return p
}
func snapshot(t *testing.T, st *store.Store) string {
	t.Helper()
	db, e := sql.Open("sqlite", st.Path())
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	h := sha256.New()
	enc := json.NewEncoder(h)
	for _, table := range []string{"content_items", "content_versions", "publications", "publication_events", "comments", "moderation_events"} {
		h.Write([]byte(table))
		rows, e := db.Query("SELECT * FROM " + table + " ORDER BY 1")
		if e != nil {
			t.Fatal(e)
		}
		cols, e := rows.Columns()
		if e != nil {
			t.Fatal(e)
		}
		for rows.Next() {
			v := make([]any, len(cols))
			p := make([]any, len(cols))
			for i := range v {
				p[i] = &v[i]
			}
			if e := rows.Scan(p...); e != nil {
				t.Fatal(e)
			}
			if e := enc.Encode(v); e != nil {
				t.Fatal(e)
			}
		}
		if e := rows.Err(); e != nil {
			t.Fatal(e)
		}
		rows.Close()
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func TestScopesCostsMetricsAndExamples(t *testing.T) {
	st, svc, _, bundle := open(t, generate(t))
	ctx := context.Background()
	for uid, scope := range map[int64]string{1: "1=1", 2: "p.account_id<=6", 3: "p.account_id<=6", 5: "p.account_id<=3", 7: "p.account_id>=7", 17: "p.account_id=1", 18: "0=1", 4: "c.editor_id=4", 6: "c.partner_id=6"} {
		for _, pair := range [][2]string{
			{"SELECT COUNT(*) AS n FROM publications", "SELECT COUNT(*) AS n FROM publications p JOIN content_versions v ON v.id=p.version_id JOIN content_items c ON c.id=v.content_id WHERE " + scope},
			{"SELECT COALESCE(SUM(views),0) AS n FROM publication_daily_metrics", "SELECT COALESCE(SUM(m.views),0) AS n FROM publication_daily_metrics m JOIN publications p ON p.id=m.publication_id JOIN content_versions v ON v.id=p.version_id JOIN content_items c ON c.id=v.content_id WHERE " + scope},
		} {
			got, e := svc.Query(ctx, pair[0], uid, 1)
			if e != nil {
				t.Fatal(e)
			}
			if got.Rows[0]["n"] != count(t, st, pair[1]) {
				t.Fatalf("uid=%d got=%v query=%s", uid, got.Rows, pair[0])
			}
		}
		for _, ex := range bundle.Database.SQLExamples {
			_, e := svc.Query(ctx, ex.SQL, uid, 100)
			if e != nil && !rewrite.IsDenied(e) {
				t.Fatalf("uid=%d example=%s: %v", uid, ex.Label, e)
			}
		}
	}
	for _, uid := range []int64{4, 5, 6, 17} {
		if _, e := svc.Query(ctx, "SELECT SUM(spend_cents) FROM ad_daily_metrics", uid, 1); e == nil {
			t.Fatalf("costs exposed uid=%d", uid)
		}
	}
	for _, uid := range []int64{20, 9999} {
		if _, e := svc.Query(ctx, "SELECT * FROM platforms", uid, 10); e == nil {
			t.Fatal("invalid identity accepted")
		}
	}
	got, e := svc.Query(ctx, "SELECT COUNT(*) AS n FROM comments", 6, 1)
	if e != nil || got.Rows[0]["n"] != int64(0) {
		t.Fatalf("partner internal comments: %v %v", got, e)
	}
	got, e = svc.Query(ctx, "SELECT COUNT(*) AS n FROM publications WHERE account_id=12 OR 1=1", 5, 1)
	want := count(t, st, "SELECT COUNT(*) AS n FROM publications WHERE account_id<=3")
	if e != nil || got.Rows[0]["n"] != want {
		t.Fatalf("OR widened scope: %v %v", got, e)
	}
	if _, e = svc.QueryWithParameters(ctx, "SELECT * FROM publications", 6, 10, map[string]any{"userId": float64(1)}); e == nil {
		t.Fatal("identity injection accepted")
	}
	for _, day := range []string{"2026-09-30", "2026-10-01"} {
		svc.AsOf, _ = time.Parse("2006-01-02", day)
		r, e := svc.Query(ctx, "SELECT COUNT(*) AS n FROM publications", 18, 1)
		if e != nil {
			t.Fatal(e)
		}
		n := r.Rows[0]["n"].(int64)
		if day == "2026-09-30" && n == 0 || day == "2026-10-01" && n != 0 {
			t.Fatalf("grant boundary %s: %d", day, n)
		}
	}
}

func TestActionsPreviewCommitAndBoundaries(t *testing.T) {
	source := generate(t)
	st, _, _, _ := open(t, copyDB(t, source))
	draft := raw(t, st, "SELECT v.id,c.editor_id,c.team_id FROM content_versions v JOIN content_items c ON c.id=v.content_id WHERE v.state='draft' AND c.team_id=1 AND NOT EXISTS(SELECT 1 FROM publications p WHERE p.version_id=v.id AND p.status<>'draft') ORDER BY v.id LIMIT 1")
	submitted := raw(t, st, "SELECT v.id,c.team_id,v.author_id FROM content_versions v JOIN content_items c ON c.id=v.content_id WHERE v.state='submitted' AND c.team_id=1 ORDER BY v.id LIMIT 1")
	schedule := raw(t, st, "SELECT p.id,p.account_id FROM publications p JOIN content_versions v ON v.id=p.version_id JOIN content_items c ON c.id=v.content_id JOIN campaigns x ON x.id=c.campaign_id JOIN channel_accounts a ON a.id=p.account_id WHERE p.status='draft' AND v.state='approved' AND x.end_date>'2026-11-15' AND a.status='active' AND p.account_id<=6 ORDER BY p.id LIMIT 1")
	future := raw(t, st, "SELECT p.id,p.account_id FROM publications p JOIN channel_accounts a ON a.id=p.account_id WHERE p.status='scheduled' AND a.status='active' AND p.account_id<=6 AND p.scheduled_at<'2026-11-18' ORDER BY p.id LIMIT 1")
	comment := raw(t, st, "SELECT c.id,p.account_id FROM comments c JOIN publications p ON p.id=c.publication_id WHERE c.state='pending' AND p.status='published' AND p.account_id<=3 ORDER BY c.id LIMIT 1")
	owner := raw(t, st, "SELECT id,editor_id,version FROM content_items WHERE team_id=1 AND editor_id<>10 ORDER BY id LIMIT 1")
	type trial struct {
		name, action string
		uid          int64
		args         map[string]any
		denial       string
		day          string
		setup        []string
	}
	cases := []trial{
		{"submit", "submit_version", draft["editor_id"].(int64), map[string]any{"version_id": draft["id"]}, "", "", nil},
		{"wrong_editor", "submit_version", 8, map[string]any{"version_id": draft["id"]}, "分配给本人", "", nil},
		{"inactive", "submit_version", 20, map[string]any{"version_id": draft["id"]}, "有效运营身份", "", nil},
		{"review_approve", "review_version", 2, map[string]any{"version_id": submitted["id"], "decision": "approved"}, "", "", nil},
		{"review_reject", "review_version", 2, map[string]any{"version_id": submitted["id"], "decision": "rejected"}, "", "", nil},
		{"cross_team_review", "review_version", 7, map[string]any{"version_id": submitted["id"], "decision": "approved"}, "所属团队负责人", "", nil},
		{"self_review", "review_version", 2, map[string]any{"version_id": submitted["id"], "decision": "approved"}, "作者不能审核", "", []string{fmt.Sprintf("UPDATE content_versions SET author_id=2 WHERE id=%d", submitted["id"])}},
		{"schedule", "schedule_publication", 2, map[string]any{"publication_id": schedule["id"], "scheduled_at": "2026-11-15T09:00:00+08:00"}, "", "", nil},
		{"unapproved", "schedule_publication", 2, map[string]any{"publication_id": schedule["id"], "scheduled_at": "2026-11-15T09:00:00+08:00"}, "已审核内容", "", []string{fmt.Sprintf("UPDATE content_versions SET state='draft',reviewed_by=NULL,review_note=NULL WHERE id=(SELECT version_id FROM publications WHERE id=%d)", schedule["id"])}},
		{"past_schedule", "schedule_publication", 2, map[string]any{"publication_id": schedule["id"], "scheduled_at": "2026-11-01T09:00:00+08:00"}, "未来时间", "", nil},
		{"read_only_operator", "schedule_publication", 17, map[string]any{"publication_id": schedule["id"], "scheduled_at": "2026-11-15T09:00:00+08:00"}, "OPERATE", "", nil},
		{"partner_cannot_schedule", "schedule_publication", 6, map[string]any{"publication_id": schedule["id"], "scheduled_at": "2026-11-15T09:00:00+08:00"}, "账号运营角色", "", nil},
		{"suspended", "schedule_publication", 2, map[string]any{"publication_id": schedule["id"], "scheduled_at": "2026-11-15T09:00:00+08:00"}, "账号暂停", "", []string{fmt.Sprintf("UPDATE channel_accounts SET status='suspended' WHERE id=%d", schedule["account_id"])}},
		{"record_success", "record_publication_result", 2, map[string]any{"publication_id": future["id"], "result": "published"}, "", "2026-11-18", nil},
		{"record_failure", "record_publication_result", 2, map[string]any{"publication_id": future["id"], "result": "failed"}, "", "2026-11-18", nil},
		{"too_early", "record_publication_result", 2, map[string]any{"publication_id": future["id"], "result": "published"}, "尚未到发布时间", "", nil},
		{"approve_comment", "moderate_comment", 5, map[string]any{"comment_id": comment["id"], "decision": "approve", "reply_text": ""}, "", "", nil},
		{"hide_comment", "moderate_comment", 5, map[string]any{"comment_id": comment["id"], "decision": "hide", "reply_text": ""}, "", "", nil},
		{"reply_comment", "moderate_comment", 5, map[string]any{"comment_id": comment["id"], "decision": "reply", "reply_text": "请查看活动页的报名条件。"}, "", "", nil},
		{"blank_reply", "moderate_comment", 5, map[string]any{"comment_id": comment["id"], "decision": "reply", "reply_text": " "}, "回复文本", "", nil},
		{"wrong_operator", "moderate_comment", 15, map[string]any{"comment_id": comment["id"], "decision": "hide", "reply_text": ""}, "OPERATE", "", nil},
		{"handled_comment", "moderate_comment", 5, map[string]any{"comment_id": comment["id"], "decision": "hide", "reply_text": ""}, "待审评论", "", []string{fmt.Sprintf("UPDATE comments SET state='visible' WHERE id=%d", comment["id"])}},
		{"reassign", "reassign_editor", 2, map[string]any{"content_id": owner["id"], "editor_id": int64(10), "expected_version": owner["version"]}, "", "", nil},
		{"stale_assignment", "reassign_editor", 2, map[string]any{"content_id": owner["id"], "editor_id": int64(10), "expected_version": int64(9999)}, "分工已变化", "", nil},
		{"wrong_team_editor", "reassign_editor", 2, map[string]any{"content_id": owner["id"], "editor_id": int64(8), "expected_version": owner["version"]}, "同团队有效编辑", "", nil},
		{"analyst_cannot_assign", "reassign_editor", 3, map[string]any{"content_id": owner["id"], "editor_id": int64(10), "expected_version": owner["version"]}, "所属团队负责人", "", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := copyDB(t, source)
			st, _, rw, b := open(t, path)
			ctx := context.Background()
			if len(c.setup) > 0 {
				db, e := sql.Open("sqlite", path)
				if e != nil {
					t.Fatal(e)
				}
				for _, q := range c.setup {
					if _, e = db.Exec(q); e != nil {
						db.Close()
						t.Fatal(e)
					}
				}
				db.Close()
			}
			day := c.day
			if day == "" {
				day = "2026-11-13"
			}
			now, _ := time.Parse(time.RFC3339, day+"T04:00:00Z")
			runner := action.Runner{Store: st, Rewriter: rw, Libraries: b.Database.ActionLibraries, UserID: c.uid, Now: now, Bindings: map[string]any{"userId": c.uid, "asOf": now.Format(time.RFC3339)}}
			var def action.Definition
			for _, d := range b.Database.Actions {
				if d.Name == c.action {
					def = d
				}
			}
			c.args["reason"] = "渠道运营回归验收"
			before := snapshot(t, st)
			preview, e := runner.Run(ctx, def, c.args, true, nil)
			if snapshot(t, st) != before {
				t.Fatal("preview or denial modified data")
			}
			if c.denial != "" {
				if e == nil || !strings.Contains(e.Error(), c.denial) {
					t.Fatalf("expected %q, got %v", c.denial, e)
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			done, e := runner.Run(ctx, def, c.args, false, preview)
			if e != nil {
				t.Fatal(e)
			}
			if !reflect.DeepEqual(done, preview) {
				t.Fatal("confirmation changed plan")
			}
			if snapshot(t, st) == before {
				t.Fatal("commit did not change data")
			}
			// A repeated transition must fail rather than append duplicate audit events.
			if _, e = runner.Run(ctx, def, c.args, false, nil); e == nil {
				t.Fatal("repeated transition accepted")
			}
		})
	}
}

package academic_test

import (
	"context"
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

func generate(t *testing.T, day string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "academic.sqlite3")
	if out, err := exec.Command("python3", "../../../examples/academic/generate.py", "--db", path, "--as-of", day).CombinedOutput(); err != nil {
		t.Fatalf("generate: %s %v", out, err)
	}
	return path
}
func open(t *testing.T, path string) (*store.Store, *service.QueryService, *rewrite.Rewriter, *configbundle.Bundle) {
	t.Helper()
	ctx := context.Background()
	st, err := store.OpenWriter(ctx, path, 20*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	bundle, err := configbundle.Load("../../../examples/academic/bundle.yml")
	if err != nil {
		t.Fatal(err)
	}
	svc, err := service.New(ctx, st, bundle.Database.Config, time.Date(2026, 11, 13, 4, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	names, err := st.TableNames(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rw, err := rewrite.New(bundle.Database.Config, names)
	if err != nil {
		t.Fatal(err)
	}
	return st, svc, rw, bundle
}
func raw(t *testing.T, st *store.Store, q string) map[string]any {
	t.Helper()
	r, err := st.RawQuery(context.Background(), q, 1)
	if err != nil || r.RowCount != 1 {
		t.Fatalf("%s: %v %v", q, r, err)
	}
	return r.Rows[0]
}
func number(t *testing.T, st *store.Store, q string) int64 {
	t.Helper()
	for _, v := range raw(t, st, q) {
		return v.(int64)
	}
	t.Fatal("no value")
	return 0
}

func TestRoleScopesPublicationAndExamples(t *testing.T) {
	st, svc, _, bundle := open(t, generate(t, "2026-11-13"))
	ctx := context.Background()
	for _, uid := range []int64{1, 2, 1001, 1002, 2001, 2002, 10001, 10076} {
		scope := fmt.Sprintf("SELECT COUNT(*) AS n FROM students s JOIN users u ON u.id=s.id WHERE %s", map[int64]string{
			1: "1=1", 2: "u.school_id=1", 1001: "EXISTS(SELECT 1 FROM enrollments e JOIN section_teachers t ON t.section_id=e.section_id WHERE e.student_id=s.id AND e.status<>'dropped' AND t.teacher_id=1001)", 1002: "EXISTS(SELECT 1 FROM enrollments e JOIN section_teachers t ON t.section_id=e.section_id WHERE e.student_id=s.id AND e.status<>'dropped' AND t.teacher_id=1002)", 2001: "s.class_id IN(1,2,3)", 2002: "s.class_id IN(4,5,6,7,8)", 10001: "s.id=10001", 10076: "s.id=10076",
		}[uid])
		got, err := svc.Query(ctx, "SELECT COUNT(*) AS n FROM students", uid, 1)
		if err != nil {
			t.Fatal(err)
		}
		if got.Rows[0]["n"] != number(t, st, scope) {
			t.Fatalf("uid=%d scope=%v", uid, got.Rows)
		}
		for _, ex := range bundle.Database.SQLExamples {
			if _, err := svc.Query(ctx, ex.SQL, uid, 100); err != nil {
				t.Fatalf("uid=%d example=%s: %v", uid, ex.Label, err)
			}
		}
	}
	for _, uid := range []int64{9999, 1080} {
		if _, err := svc.Query(ctx, "SELECT * FROM courses", uid, 10); err == nil {
			t.Fatalf("invalid identity %d accepted", uid)
		}
	}
	for _, uid := range []int64{10001, 2001} {
		got, err := svc.Query(ctx, "SELECT COUNT(*) AS n FROM grades g LEFT JOIN grade_releases r ON r.assessment_id=g.assessment_id WHERE r.assessment_id IS NULL", uid, 1)
		if err != nil || got.Rows[0]["n"] != int64(0) {
			t.Fatalf("draft leaked: %v %v", got, err)
		}
	}
	drafts, err := svc.Query(ctx, "SELECT COUNT(*) AS n FROM grades g LEFT JOIN grade_releases r ON r.assessment_id=g.assessment_id WHERE r.assessment_id IS NULL", 1001, 1)
	if err != nil || drafts.Rows[0]["n"].(int64) == 0 {
		t.Fatalf("teacher drafts missing: %v %v", drafts, err)
	}
	contacts, err := svc.Query(ctx, "SELECT COUNT(*) AS n FROM student_contacts", 1001, 1)
	if err != nil || contacts.Rows[0]["n"] != int64(0) {
		t.Fatalf("teacher contacts: %v %v", contacts, err)
	}
	if _, err := svc.Query(ctx, "SELECT emergency_contact FROM student_contacts", 1, 1); err == nil {
		t.Fatal("emergency contact exposed")
	}
	isolated, err := svc.Query(ctx, "SELECT COUNT(*) AS n FROM enrollments WHERE section_id NOT IN(SELECT section_id FROM section_teachers WHERE teacher_id=1001)", 1001, 1)
	if err != nil || isolated.Rows[0]["n"] != int64(0) {
		t.Fatalf("teacher gained other course records: %v %v", isolated, err)
	}
	// OR does not widen a student's scope; a supplied identity is never trusted.
	got, err := svc.Query(ctx, "SELECT COUNT(*) AS n FROM students WHERE id=10002 OR 1=1", 10001, 1)
	if err != nil || got.Rows[0]["n"] != int64(1) {
		t.Fatalf("OR scope: %v %v", got, err)
	}
	if _, err := svc.QueryWithParameters(ctx, "SELECT * FROM students", 10001, 10, map[string]any{"userId": float64(1)}); err == nil {
		t.Fatal("identity parameter accepted")
	}
	for _, date := range []string{"2026-09-30", "2026-10-01"} {
		svc.AsOf, _ = time.Parse("2006-01-02", date)
		got, err := svc.Query(ctx, "SELECT COUNT(*) AS n FROM students WHERE class_id=4", 2001, 1)
		want := int64(25)
		if date == "2026-10-01" {
			want = 0
		}
		if err != nil || got.Rows[0]["n"] != want {
			t.Fatalf("counselor handover %s: %v %v", date, got, err)
		}
	}
}

func snapshot(t *testing.T, st *store.Store) string {
	t.Helper()
	out := map[string]any{}
	db, err := sql.Open("sqlite", st.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, table := range []string{"sections", "section_meetings", "enrollments", "grades", "grade_releases"} {
		// Test snapshots must include all rows rather than the service row limit.
		rows, err := db.Query("SELECT * FROM " + table + " ORDER BY 1")
		if err != nil {
			t.Fatal(err)
		}
		cols, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		values := [][]any{}
		for rows.Next() {
			row := make([]any, len(cols))
			ptr := make([]any, len(cols))
			for i := range row {
				ptr[i] = &row[i]
			}
			if err := rows.Scan(ptr...); err != nil {
				t.Fatal(err)
			}
			values = append(values, row)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		out[table] = values
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func copyDB(t *testing.T, source string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "case.sqlite3")
	a, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.Copy(b, a); err != nil {
		t.Fatal(err)
	}
	if err = b.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestActionsPreviewCommitAndDenials(t *testing.T) {
	for _, day := range []string{"2026-09-10", "2026-11-13"} {
		source := generate(t, day)
		t.Run(day, func(t *testing.T) {
			st, _, _, _ := open(t, copyDB(t, source))
			pending := raw(t, st, "SELECT g.id,t.teacher_id,a.id AS aid,e.student_id FROM grades g JOIN assessments a ON a.id=g.assessment_id JOIN sections s ON s.id=a.section_id JOIN section_teachers t ON t.section_id=s.id AND t.is_lead=1 JOIN enrollments e ON e.id=g.enrollment_id LEFT JOIN grade_releases r ON r.assessment_id=a.id WHERE s.term_id=2 AND a.kind='midterm' AND r.assessment_id IS NULL ORDER BY g.id LIMIT 1")
			enrollment := raw(t, st, "SELECT e.id,e.student_id,e.section_id FROM enrollments e JOIN sections s ON s.id=e.section_id JOIN courses c ON c.id=s.course_id WHERE s.term_id=2 AND e.status='enrolled' AND c.kind='elective' ORDER BY e.id LIMIT 1")
			emptySection := number(t, st, "SELECT s.id FROM sections s WHERE s.term_id=2 AND s.course_id=20 AND s.status='open' AND NOT EXISTS(SELECT 1 FROM enrollments e WHERE e.section_id=s.id) ORDER BY s.id LIMIT 1")
			advancedSection := number(t, st, "SELECT id FROM sections WHERE term_id=2 AND course_id=5 AND status='open' ORDER BY id LIMIT 1")
			published := raw(t, st, "SELECT g.id,t.teacher_id FROM grades g JOIN assessments a ON a.id=g.assessment_id JOIN grade_releases r ON r.assessment_id=a.id JOIN section_teachers t ON t.section_id=a.section_id AND t.is_lead=1 ORDER BY g.id LIMIT 1")
			room := raw(t, st, "SELECT m.id,m.room_id,s.course_id,c.school_id,r.id AS target FROM section_meetings m JOIN sections s ON s.id=m.section_id JOIN courses c ON c.id=s.course_id JOIN rooms r ON r.room_type=c.room_type AND r.capacity>=s.capacity WHERE s.term_id=2 AND s.status='open' AND r.id<>m.room_id AND NOT EXISTS(SELECT 1 FROM section_meetings other JOIN sections x ON x.id=other.section_id WHERE x.term_id=2 AND x.status<>'cancelled' AND other.room_id=r.id AND other.weekday=m.weekday AND other.period_from<=m.period_to AND other.period_to>=m.period_from) ORDER BY m.id,r.id LIMIT 1")
			type trial struct {
				name, action string
				uid          int64
				args         map[string]any
				denied       bool
				prepareDrop  bool
			}
			cases := []trial{
				{"record_grade", "record_grade", pending["teacher_id"].(int64), map[string]any{"grade_id": pending["id"], "score": float64(88), "status": "graded"}, day == "2026-09-10", false},
				{"publish", "publish_grades", pending["teacher_id"].(int64), map[string]any{"assessment_id": pending["aid"]}, day == "2026-09-10", false},
				{"student_cannot_grade", "record_grade", pending["student_id"].(int64), map[string]any{"grade_id": pending["id"], "score": float64(88), "status": "graded"}, true, false},
				{"registrar_cannot_publish", "publish_grades", 1, map[string]any{"assessment_id": pending["aid"]}, true, false},
				{"drop", "drop_section", enrollment["student_id"].(int64), map[string]any{"enrollment_id": enrollment["id"]}, day == "2026-11-13", false},
				{"other_student_cannot_drop", "drop_section", 11000, map[string]any{"enrollment_id": enrollment["id"]}, true, false},
				{"reenroll", "enroll_section", enrollment["student_id"].(int64), map[string]any{"section_id": enrollment["section_id"]}, day == "2026-11-13", day == "2026-09-10"},
				{"duplicate_course", "enroll_section", enrollment["student_id"].(int64), map[string]any{"section_id": enrollment["section_id"]}, true, false},
				{"change_room", "change_meeting_room", 1, map[string]any{"meeting_id": room["id"], "room_id": room["target"], "expected_room_id": room["room_id"]}, false, false},
				{"teacher_cannot_change_room", "change_meeting_room", 1001, map[string]any{"meeting_id": room["id"], "room_id": room["target"], "expected_room_id": room["room_id"]}, true, false},
				{"stale_room", "change_meeting_room", 1, map[string]any{"meeting_id": room["id"], "room_id": room["target"], "expected_room_id": int64(9999)}, true, false},
				{"fresh_enroll", "enroll_section", 10076, map[string]any{"section_id": emptySection}, day == "2026-11-13", false},
				{"full_class", "enroll_section", 10076, map[string]any{"section_id": emptySection}, true, false},
				{"cancelled_class", "enroll_section", 10076, map[string]any{"section_id": emptySection}, true, false},
				{"missing_prerequisite", "enroll_section", 10076, map[string]any{"section_id": advancedSection}, true, false},
				{"credit_limit", "enroll_section", 10076, map[string]any{"section_id": emptySection}, true, false},
				{"student_timetable_conflict", "enroll_section", 10076, map[string]any{"section_id": emptySection}, true, false},
				{"wrong_teacher", "record_grade", 1033, map[string]any{"grade_id": pending["id"], "score": float64(88), "status": "graded"}, true, false},
				{"released_grade_locked", "record_grade", published["teacher_id"].(int64), map[string]any{"grade_id": published["id"], "score": float64(88), "status": "graded"}, true, false},
				{"incomplete_publication", "publish_grades", pending["teacher_id"].(int64), map[string]any{"assessment_id": pending["aid"]}, true, false},
			}
			for _, c := range cases {
				t.Run(c.name, func(t *testing.T) {
					st, _, rw, bundle := open(t, copyDB(t, source))
					ctx := context.Background()
					// Deliberately prepare individual boundary conditions in the isolated copy.
					setup := map[string][]string{
						"full_class":                 {fmt.Sprintf("UPDATE sections SET capacity=1 WHERE id=%d", emptySection), fmt.Sprintf("INSERT INTO enrollments(student_id,section_id,status,enrolled_on) VALUES(11000,%d,'enrolled','2026-09-02')", emptySection)},
						"cancelled_class":            {fmt.Sprintf("UPDATE sections SET status='cancelled' WHERE id=%d", emptySection)},
						"credit_limit":               {"UPDATE terms SET max_credits=18 WHERE id=2"},
						"student_timetable_conflict": {fmt.Sprintf("UPDATE section_meetings SET weekday=(SELECT m.weekday FROM enrollments e JOIN sections s ON s.id=e.section_id JOIN section_meetings m ON m.section_id=s.id WHERE e.student_id=10076 AND s.term_id=2 AND e.status='enrolled' LIMIT 1),period_from=1,period_to=12 WHERE section_id=%d", emptySection)},
						"incomplete_publication":     {fmt.Sprintf("UPDATE grades SET score=NULL,status='pending',graded_by=NULL,graded_on=NULL WHERE id=%d", pending["id"].(int64))},
					}[c.name]
					if len(setup) > 0 {
						db, err := sql.Open("sqlite", st.Path())
						if err != nil {
							t.Fatal(err)
						}
						for _, q := range setup {
							if _, err := db.Exec(q); err != nil {
								db.Close()
								t.Fatal(err)
							}
						}
						db.Close()
					}
					defs := map[string]action.Definition{}
					for _, d := range bundle.Database.Actions {
						defs[d.Name] = d
					}
					now, _ := time.Parse(time.RFC3339, day+"T04:00:00Z")
					runner := action.Runner{Store: st, Rewriter: rw, Libraries: bundle.Database.ActionLibraries, UserID: c.uid, Now: now, Bindings: map[string]any{"userId": c.uid, "asOf": now.Format(time.RFC3339)}}
					if c.prepareDrop {
						if _, err := runner.Run(ctx, defs["drop_section"], map[string]any{"enrollment_id": enrollment["id"], "reason": "重新选课准备"}, false, nil); err != nil {
							t.Fatal(err)
						}
					}
					c.args["reason"] = "教务回归验收"
					before := snapshot(t, st)
					preview, err := runner.Run(ctx, defs[c.action], c.args, true, nil)
					if snapshot(t, st) != before {
						t.Fatal("preview/denial wrote data")
					}
					if c.denied {
						if err == nil {
							t.Fatal("expected denial")
						}
						message := map[string]string{"wrong_teacher": "需要本教学班授课教师", "released_grade_locked": "成绩已发布并锁定"}[c.name]
						if day == "2026-09-10" {
							if v := map[string]string{"full_class": "教学班已满", "missing_prerequisite": "先修课程", "credit_limit": "学分上限", "student_timetable_conflict": "时间冲突"}[c.name]; v != "" {
								message = v
							}
						}
						if day == "2026-11-13" && c.name == "incomplete_publication" {
							message = "成绩尚未齐备"
						}
						if message != "" && !strings.Contains(err.Error(), message) {
							t.Fatalf("wrong denial: %v; want %s", err, message)
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					done, err := runner.Run(ctx, defs[c.action], c.args, false, preview)
					if err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(done, preview) {
						t.Fatal("confirmed result differs")
					}
					if snapshot(t, st) == before {
						t.Fatal("commit did not change data")
					}
				})
			}
		})
	}
}

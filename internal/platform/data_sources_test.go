package platform_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"datascope-server/internal/application"
	"datascope-server/internal/datasource"
	"datascope-server/internal/management"
	"datascope-server/internal/store"
)

func importFixture(t *testing.T) (*fixture, datasource.Connection) {
	t.Helper()
	f := setup(t)
	t.Cleanup(f.app.DataSourceManager().Close)
	path := filepath.Join(f.dir, "external.sqlite")
	db, err := sql.Open("sqlite", path)
	must(t, err)
	_, err = db.Exec(`CREATE TABLE records(id INTEGER PRIMARY KEY,name TEXT); WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<75) INSERT INTO records SELECT x,' item '||x||' ' FROM n;`)
	must(t, err)
	must(t, db.Close())
	target, err := sql.Open("sqlite", filepath.Join(f.dir, "business.sqlite"))
	must(t, err)
	_, err = target.Exec(`CREATE TABLE imported(id INTEGER PRIMARY KEY,name TEXT)`)
	must(t, err)
	must(t, target.Close())
	c := datasource.Connection{ID: "external", Name: "External", Driver: "sqlite", Path: path}
	_, err = f.app.Call(f.ctx, "save_source_connection", application.Input{DatabaseID: "default", SourceConnection: &c})
	must(t, err)
	return f, c
}
func saveImport(t *testing.T, f *fixture, id, script string) *datasource.Task {
	t.Helper()
	job := datasource.Task{ID: id, Name: id, Script: script, Sources: []string{"external"}, Schedule: datasource.Schedule{Mode: "manual", Timezone: "Asia/Shanghai"}, Timeout: 5}
	value, err := f.app.Call(f.ctx, "save_import_task", application.Input{DatabaseID: "default", ImportTask: &job})
	must(t, err)
	return value.(*datasource.Task)
}
func awaitImport(t *testing.T, f *fixture, id string) datasource.Run {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		value, err := f.app.Call(f.ctx, "list_data_sources", application.Input{DatabaseID: "default"})
		must(t, err)
		for _, r := range value.(*datasource.Overview).Runs {
			if r.ID == id && r.Status != "running" {
				full, e := f.app.DataSourceManager().GetRun(f.ctx, "default", id)
				must(t, e)
				return *full
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("import did not finish")
	return datasource.Run{}
}
func countImported(t *testing.T, f *fixture) int {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(f.dir, "business.sqlite"))
	must(t, err)
	defer db.Close()
	var count int
	must(t, db.QueryRow("SELECT COUNT(*) FROM imported").Scan(&count))
	return count
}

func TestDataSourceImportRollbackCheckpointAndPermissions(t *testing.T) {
	f, c := importFixture(t)
	script := `function main(params, context) {
 const source=sources.get("external");const target=db.target();
 const rows=source.query("SELECT id,name FROM records ORDER BY id LIMIT 100");
 for(const row of rows) {
   const existing=target.query("SELECT id FROM imported WHERE id=?",[row.id]);
   if(existing.length===0) target.update("INSERT INTO imported(id,name) VALUES(?,?)",[row.id,row.name.trim()]);
 }
 context.log("imported "+rows.length);
 context.saveCheckpoint({lastId:rows[rows.length-1].id});
}`
	task := saveImport(t, f, "customers", script)
	_, err := f.app.Call(f.readerContext(), "list_data_sources", application.Input{DatabaseID: "default"})
	if !errors.Is(err, management.ErrDenied) {
		t.Fatalf("reader accepted: %v", err)
	}
	_, err = f.app.Call(f.ctx, "delete_source_connection", application.Input{DatabaseID: "default", ID: c.ID})
	if err == nil {
		t.Fatal("referenced source deleted")
	}
	value, err := f.app.Call(f.ctx, "trial_import_task", application.Input{DatabaseID: "default", ID: task.ID})
	must(t, err)
	trial := awaitImport(t, f, value.(*datasource.Run).ID)
	if trial.Status != "rolled_back" || trial.Read != 75 || trial.Written != 75 || countImported(t, f) != 0 {
		t.Fatalf("trial: %+v", trial)
	}
	value, err = f.app.Call(f.ctx, "run_import_task", application.Input{DatabaseID: "default", ID: task.ID})
	must(t, err)
	run := awaitImport(t, f, value.(*datasource.Run).ID)
	if run.Status != "succeeded" || countImported(t, f) != 75 || run.Checkpoint["lastId"] != float64(75) {
		t.Fatalf("run: %+v", run)
	}
	// A second run must be able to implement idempotence in JS.
	value, err = f.app.Call(f.ctx, "run_import_task", application.Input{DatabaseID: "default", ID: task.ID})
	must(t, err)
	run = awaitImport(t, f, value.(*datasource.Run).ID)
	if run.Status != "succeeded" || run.Written != 0 || countImported(t, f) != 75 {
		t.Fatalf("replay: %+v", run)
	}
	// Config and history survive constructing a new runtime.
	peer := datasource.New(f.m, f.c)
	defer peer.Close()
	overview, err := peer.List(context.Background(), "default")
	must(t, err)
	if len(overview.Tasks) != 1 || len(overview.Runs) != 3 || len(overview.Tasks[0].Checkpoint) == 0 {
		t.Fatalf("lost persistent state: %+v", overview)
	}
}

func TestImportFailureStopAndIsolation(t *testing.T) {
	f, _ := importFixture(t)
	for _, test := range []struct{ name, script string }{
		{"throw", `function main(){db.target().update("INSERT INTO imported(id,name) VALUES(?,?)",[1,"x"]);throw new Error("no");}`},
		{"caught_sql", `function main(){db.target().update("INSERT INTO imported(id,name) VALUES(?,?)",[1,"x"]);try{db.target().update("INSERT INTO absent VALUES(?)",[1]);}catch(e){}}`},
		{"source_write", `function main(){sources.get("external").query("DELETE FROM records WHERE id=1");}`},
		{"unselected", `function main(){sources.get("other");}`},
		{"promise", `async function main(){db.target().update("INSERT INTO imported(id,name) VALUES(?,?)",[1,"x"]);}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			task := saveImport(t, f, test.name, test.script)
			run, err := f.app.DataSourceManager().Start(f.ctx, "default", task.ID, f.admin.ID, false, false, nil)
			must(t, err)
			finished := awaitImport(t, f, run.ID)
			if finished.Status != "failed" || countImported(t, f) != 0 {
				t.Fatalf("unsafe run: %+v", finished)
			}
		})
	}
	task := saveImport(t, f, "stop", `function main(){db.target().update("INSERT INTO imported(id,name) VALUES(?,?)",[1,"x"]);while(true){}}`)
	run, err := f.app.DataSourceManager().Start(f.ctx, "default", task.ID, f.admin.ID, false, false, nil)
	must(t, err)
	_, err = f.app.DataSourceManager().Start(f.ctx, "default", task.ID, f.admin.ID, false, false, nil)
	if err == nil {
		t.Fatal("overlap accepted")
	}
	must(t, f.app.DataSourceManager().Stop(f.ctx, "default", run.ID))
	finished := awaitImport(t, f, run.ID)
	if finished.Status != "stopped" || countImported(t, f) != 0 {
		t.Fatalf("stop: %+v", finished)
	}
	// Internal state and its aliases can never be registered as an import source.
	statePath := filepath.Join(f.dir, "databases", "default", "data-sources", "state.db")
	_, err = f.app.DataSourceManager().SaveConnection(f.ctx, "default", datasource.Connection{ID: "internal", Name: "internal", Driver: "sqlite", Path: statePath}, "")
	if err == nil {
		t.Fatal("internal state accepted as source")
	}
}

func TestSourceCredentialsAndSchedule(t *testing.T) {
	f, _ := importFixture(t)
	c := datasource.Connection{ID: "crm", Name: "CRM", Driver: "mysql", Connection: &store.Connection{Host: "db.example", Port: 3306, Database: "business", Schema: "business", Username: "reader", TLSMode: "verify-full"}}
	password := "source-secret-do-not-return"
	saved, err := f.app.DataSourceManager().SaveConnection(f.ctx, "default", c, password)
	must(t, err)
	raw, err := json.Marshal(saved)
	must(t, err)
	if strings.Contains(string(raw), password) || !saved.HasPassword {
		t.Fatal("credential leak")
	}
	// Blank passwords preserve the encrypted secret; stale edits are rejected.
	saved.Name = "Renamed"
	_, err = f.app.DataSourceManager().SaveConnection(f.ctx, "default", *saved, "")
	must(t, err)
	_, err = f.app.DataSourceManager().SaveConnection(f.ctx, "default", *saved, "")
	if err == nil {
		t.Fatal("stale edit accepted")
	}
	now := time.Date(2026, 11, 13, 17, 0, 0, 0, time.UTC)
	next, err := (datasource.Schedule{Mode: "daily", Time: "02:00", Timezone: "Asia/Shanghai"}).Next(now)
	must(t, err)
	if next.Format(time.RFC3339) != "2026-11-13T18:00:00Z" {
		t.Fatalf("timezone: %s", next)
	}
	_, err = (datasource.Schedule{Mode: "interval", Minutes: 0}).Next(now)
	if err == nil {
		t.Fatal("invalid interval")
	}
	// A one-time job runs in the scheduler and is consumed exactly once.
	job := datasource.Task{ID: "scheduled", Name: "Scheduled", Script: `function main(){db.target().update("INSERT INTO imported(id,name) VALUES(?,?)",[99,"scheduled"]);}`, Enabled: true, Timeout: 5, Schedule: datasource.Schedule{Mode: "once", At: time.Now().Add(150 * time.Millisecond).UTC().Format(time.RFC3339Nano), Timezone: "UTC"}}
	_, err = f.app.DataSourceManager().SaveTask(f.ctx, "default", job, f.admin.ID)
	must(t, err)
	time.Sleep(200 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); f.app.DataSourceManager().RunScheduler(ctx) }()
	defer func() { cancel(); <-done }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		overview, e := f.app.DataSourceManager().List(f.ctx, "default")
		must(t, e)
		for _, run := range overview.Runs {
			if run.TaskID == "scheduled" && run.Status == "succeeded" {
				if overview.Tasks[0].Enabled {
					t.Fatal("once schedule still enabled")
				}
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("scheduled task not executed")
}

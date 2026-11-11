package ddl

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func fixture(t *testing.T) (Manager, *sql.DB) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "business.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`PRAGMA journal_mode=WAL;CREATE TABLE items(id INTEGER PRIMARY KEY AUTOINCREMENT,name TEXT,amount TEXT);INSERT INTO items VALUES(10,'one','12'),(20,'two','25');DELETE FROM items WHERE id=20;CREATE INDEX item_name ON items(name);CREATE TABLE events(id INTEGER);CREATE TRIGGER item_event AFTER INSERT ON items BEGIN INSERT INTO events VALUES(new.id); END;CREATE VIEW item_view AS SELECT id,name FROM items;`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return Manager{DatabaseID: "test", Path: path, StateDir: filepath.Join(dir, "state"), ActorID: 1, Revision: 1}, db
}

func TestViewTriggerPreservationAndBrokenTriggerRejection(t *testing.T) {
	m, db := fixture(t)
	ctx := context.Background()
	_, err := db.Exec(`CREATE VIEW editable AS SELECT id,name FROM items;CREATE TRIGGER editable_insert INSTEAD OF INSERT ON editable BEGIN INSERT INTO items(name,amount) VALUES(new.name,1); END;`)
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range []Change{{Operation: "update", ObjectType: "view", Name: "editable", Definition: Definition{SQL: "CREATE VIEW editable AS SELECT id,name FROM items WHERE id>0"}}, {Operation: "rename", ObjectType: "view", Name: "editable", NewName: "editable_new"}} {
		p, err := m.Preview(ctx, "", []Change{c})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = m.Apply(ctx, p.ID, fmt.Sprintf("view-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec("INSERT INTO editable_new(name) VALUES('view-write')"); err != nil {
		t.Fatal("lost view trigger:", err)
	}
	if _, err = m.Preview(ctx, "CREATE TRIGGER broken AFTER INSERT ON items BEGIN INSERT INTO events VALUES(new.missing); END", nil); err == nil {
		t.Fatal("accepted invalid trigger references")
	}
}

func TestApplyRevalidatesDataAndReceiptsRecover(t *testing.T) {
	m, db := fixture(t)
	ctx := context.Background()
	p, err := m.Preview(ctx, "", []Change{{Operation: "create", ObjectType: "constraint", ParentName: "items", Name: "unique_name", Definition: Definition{SQL: "CONSTRAINT unique_name UNIQUE (name)"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO items(name,amount) VALUES('one',42)"); err != nil {
		t.Fatal(err)
	}
	e, err := m.Apply(ctx, p.ID, "data-changed")
	if err == nil {
		t.Fatal("accepted data constraint conflict")
	}
	if e.Status != "failed" {
		t.Fatalf("%+v", e)
	}
	var query string
	db.QueryRow("SELECT sql FROM sqlite_schema WHERE name='items'").Scan(&query)
	if strings.Contains(query, "unique_name") {
		t.Fatal("failed transaction committed")
	}
	loaded, err := m.GetExecution(ctx, e.ID)
	if err != nil || loaded.Status != "failed" {
		t.Fatalf("receipt %+v %v", loaded, err)
	}
	p, err = m.Preview(ctx, "CREATE TABLE recovery(id)", nil)
	if err != nil {
		t.Fatal(err)
	}
	e, err = m.Apply(ctx, p.ID, "recover")
	if err != nil {
		t.Fatal(err)
	}
	e.Status = "running"
	e.FinishedAt = ""
	if err = m.saveExecution(e); err != nil {
		t.Fatal(err)
	}
	loaded, err = m.GetExecution(ctx, e.ID)
	if err != nil || loaded.Status != "succeeded" {
		t.Fatalf("recovery %+v %v", loaded, err)
	}
}

func TestConcurrentRetriesAndRowIDSemantics(t *testing.T) {
	m, db := fixture(t)
	ctx := context.Background()
	p, err := m.Preview(ctx, "CREATE TABLE once_only(id)", nil)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	ids := make(chan string, 2)
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e, err := m.Apply(ctx, p.ID, "same-request")
			if err != nil {
				errs <- err
				return
			}
			ids <- e.ID
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	id := ""
	for v := range ids {
		if id != "" && v != id {
			t.Fatal("duplicate execution")
		}
		id = v
	}
	_, err = db.Exec(`CREATE TABLE rowids(id INTEGER,name TEXT);INSERT INTO rowids(rowid,id,name) VALUES(91,7,'keep');`)
	if err != nil {
		t.Fatal(err)
	}
	p, err = m.Preview(ctx, "", []Change{{Operation: "update", ObjectType: "table", Name: "rowids", Definition: Definition{SQL: "CREATE TABLE rowids(id INTEGER,name TEXT,PRIMARY KEY(id))"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Apply(ctx, p.ID, "table-pk"); err != nil {
		t.Fatal(err)
	}
	var key, rowid int
	if err = db.QueryRow("SELECT id,rowid FROM rowids").Scan(&key, &rowid); err != nil || key != 7 || rowid != 7 {
		t.Fatalf("rowid alias overwrote data: %d %d %v", key, rowid, err)
	}
}

func TestGeneratedColumnsStrictAndForeignKeys(t *testing.T) {
	m, db := fixture(t)
	ctx := context.Background()
	_, err := db.Exec(`CREATE TABLE parent(a INTEGER,b INTEGER,PRIMARY KEY(a,b)) WITHOUT ROWID;INSERT INTO parent VALUES(1,2);CREATE TABLE child(id INTEGER PRIMARY KEY,a INTEGER,b INTEGER,total INTEGER GENERATED ALWAYS AS(a+b) STORED,FOREIGN KEY(a,b) REFERENCES parent(a,b)) STRICT;INSERT INTO child(id,a,b) VALUES(8,1,2);`)
	if err != nil {
		t.Fatal(err)
	}
	p, err := m.Preview(ctx, "", []Change{{Operation: "update", ObjectType: "table", Name: "child", Definition: Definition{SQL: "CREATE TABLE child(id INTEGER PRIMARY KEY,a INTEGER,b INTEGER,total INTEGER GENERATED ALWAYS AS(a+b) STORED,extra TEXT DEFAULT 'ok',FOREIGN KEY(a,b) REFERENCES parent(a,b)) STRICT"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Apply(ctx, p.ID, "generated"); err != nil {
		t.Fatal(err)
	}
	var total int
	var extra string
	if err = db.QueryRow("SELECT total,extra FROM child WHERE id=8").Scan(&total, &extra); err != nil || total != 3 || extra != "ok" {
		t.Fatalf("generated/default: %d %s %v", total, extra, err)
	}
	if _, err = m.Preview(ctx, "DROP TABLE parent", nil); err == nil {
		t.Fatal("allowed broken composite foreign key")
	}
}

func TestRebuildPreservesDataObjectsSequenceAndBackup(t *testing.T) {
	m, db := fixture(t)
	ctx := context.Background()
	p, err := m.Preview(ctx, "", []Change{{Operation: "update", ObjectType: "column", ParentName: "items", Name: "amount", Definition: Definition{Columns: []Column{{Name: "amount", Type: "INTEGER", NotNull: true}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if !p.RequiresRebuild {
		t.Fatal("no rebuild")
	}
	var typ string
	db.QueryRow(`SELECT type FROM pragma_table_info('items') WHERE name='amount'`).Scan(&typ)
	if typ != "TEXT" {
		t.Fatal("preview mutated source")
	}
	e, err := m.Apply(ctx, p.ID, "rebuild")
	if err != nil {
		t.Fatal(err)
	}
	if e.Status != "succeeded" {
		t.Fatalf("%+v", e)
	}
	var id, amount int
	var storage string
	if err = db.QueryRow("SELECT id,amount,typeof(amount) FROM items").Scan(&id, &amount, &storage); err != nil {
		t.Fatal(err)
	}
	if id != 10 || amount != 12 || storage != "integer" {
		t.Fatalf("%d %d %s", id, amount, storage)
	}
	if _, err = db.Exec("INSERT INTO items(name,amount) VALUES('three',30)"); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow("SELECT id FROM items WHERE name='three'").Scan(&id); err != nil || id != 21 {
		t.Fatalf("autoincrement lost: %d %v", id, err)
	}
	if err = db.QueryRow("SELECT id FROM events").Scan(&id); err != nil || id != 21 {
		t.Fatalf("trigger lost: %d %v", id, err)
	}
	if err = db.QueryRow("SELECT COUNT(*) FROM item_view").Scan(&id); err != nil || id != 2 {
		t.Fatalf("view lost: %d %v", id, err)
	}
	backup, err := Open(ctx, filepath.Join(m.StateDir, "backups", e.BackupID, "database.sqlite"), false)
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()
	if err = backup.QueryRow("SELECT count(*) FROM items").Scan(&id); err != nil || id != 1 {
		t.Fatalf("WAL backup lost rows: %d %v", id, err)
	}
	if retry, err := m.Apply(ctx, p.ID, "another-request"); err != nil || retry.ID != e.ID {
		t.Fatalf("duplicate plan: %+v %v", retry, err)
	}
}

func TestBatchTriggerViewsAndRollback(t *testing.T) {
	m, db := fixture(t)
	ctx := context.Background()
	p, err := m.Preview(ctx, `CREATE TABLE notes(id INTEGER PRIMARY KEY,note TEXT);CREATE TRIGGER note_event AFTER INSERT ON notes BEGIN INSERT INTO events VALUES(new.id);UPDATE notes SET note=upper(note) WHERE id=new.id;END;CREATE VIEW note_view AS SELECT * FROM notes;`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Apply(ctx, p.ID, "batch"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO notes VALUES(1,'hello')"); err != nil {
		t.Fatal(err)
	}
	var value string
	if err = db.QueryRow("SELECT note FROM note_view").Scan(&value); err != nil || value != "HELLO" {
		t.Fatalf("trigger body: %s %v", value, err)
	}
	if _, err = m.Preview(ctx, "DROP TABLE items", nil); err == nil {
		t.Fatal("accepted broken dependent view")
	}
	if _, err = m.Preview(ctx, "CREATE TABLE first(id);CREATE TABLE items(id)", nil); err == nil {
		t.Fatal("accepted failing batch")
	}
	var count int
	db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name='first'").Scan(&count)
	if count != 0 {
		t.Fatal("failed preview mutated source")
	}
}

func TestRejectDangerousSQLAndStalePlans(t *testing.T) {
	m, db := fixture(t)
	ctx := context.Background()
	for _, s := range []string{"ATTACH '/tmp/x' AS other", "PRAGMA writable_schema=ON", "INSERT INTO items(name) VALUES('bad')", "BEGIN", "CREATE TEMP TABLE bad(id)", "CREATE TABLE temp.bad(id)", "CREATE TABLE bad AS SELECT * FROM items", "CREATE VIEW bad AS SELECT * FROM other.items", "CREATE TRIGGER bad AFTER INSERT ON items BEGIN DELETE FROM sqlite_sequence;END;"} {
		if _, err := m.Preview(ctx, s, nil); err == nil {
			t.Fatalf("accepted %s", s)
		}
	}
	p, err := m.Preview(ctx, "CREATE TABLE later(id)", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("CREATE TABLE external(id)"); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Apply(ctx, p.ID, "stale"); err == nil {
		t.Fatal("accepted stale schema")
	}
	p, err = m.Preview(ctx, "CREATE TABLE another(id)", nil)
	if err != nil {
		t.Fatal(err)
	}
	other := m
	other.ActorID = 2
	if _, err = other.Apply(ctx, p.ID, "stolen"); err == nil {
		t.Fatal("accepted stolen plan")
	}
}

func TestConstraintDataValidationAndNativeNotNull(t *testing.T) {
	m, _ := fixture(t)
	ctx := context.Background()
	if _, err := m.Preview(ctx, "", []Change{{Operation: "create", ObjectType: "constraint", ParentName: "items", Name: "positive", Definition: Definition{SQL: "CONSTRAINT positive CHECK (amount < 0)"}}}); err == nil {
		t.Fatal("accepted invalid data")
	}
	p, err := m.Preview(ctx, `ALTER TABLE "items" ALTER COLUMN "amount" SET NOT NULL`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Apply(ctx, p.ID, "notnull"); err != nil {
		t.Fatal(err)
	}
	p, err = m.Preview(ctx, "", []Change{{Operation: "create", ObjectType: "constraint", ParentName: "items", Name: "positive", Definition: Definition{SQL: "CONSTRAINT positive CHECK (amount > 0)"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Apply(ctx, p.ID, "check"); err != nil {
		t.Fatal(err)
	}
	files, _ := os.ReadDir(m.StateDir)
	for _, f := range files {
		if strings.Contains(f.Name(), ".sqlite") {
			t.Fatal("preview snapshot retained")
		}
	}
}

func TestBatchNativeAlterUnicodeAndTriggerCase(t *testing.T) {
	m, _ := fixture(t)
	query := `-- leading comment
CREATE TABLE "客户"("姓名" TEXT); /* between statements */
ALTER TABLE "客户" ALTER COLUMN "姓名" SET /* constraint */ NOT NULL;
CREATE TRIGGER "客户触发器" AFTER INSERT ON "客户" BEGIN INSERT INTO events VALUES(CASE WHEN new."姓名"='a;b' THEN 1 ELSE 2 END);END;
ALTER TABLE "客户" ALTER COLUMN "姓名" DROP NOT NULL; -- final comment`
	p, err := m.Preview(context.Background(), query, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.SQL) != 4 {
		t.Fatalf("split %v", p.SQL)
	}
	if _, err = m.Apply(context.Background(), p.ID, "unicode-batch"); err != nil {
		t.Fatal(err)
	}
}

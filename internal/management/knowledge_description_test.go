package management

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestKnowledgeDescriptionMigrationAndIndex(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "management.db")+"?_pragma=busy_timeout(5000)&_txlock=immediate")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(sourceDDL + `CREATE VIRTUAL TABLE KNOWLEDGE_SEARCH USING fts5(KEY_ UNINDEXED,NAME_,SUMMARY_,CONTENT_,tokenize='trigram');CREATE TRIGGER TR_KNOWLEDGE_INSERT AFTER INSERT ON KNOWLEDGE BEGIN INSERT INTO KNOWLEDGE_SEARCH VALUES(new.KEY_,json_extract(new.DATA_,'$.name'),json_extract(new.DATA_,'$.summary'),json_extract(new.DATA_,'$.content'));END;`); err != nil {
		t.Fatal(err)
	}
	description := "使用说明第一行\nMigrationSearchToken second line\n第三行保留完整描述"
	legacy, _ := json.Marshal(map[string]any{"id": "guide", "database_id": "default", "name": "Guide", "summary": description, "content": "Body", "revision": 7, "published": true})
	if _, err = db.Exec("INSERT INTO KNOWLEDGE VALUES('default/guide',?);INSERT INTO CONFIG_VERSION VALUES('knowledge','default/guide',7,?,'2026-10-30');INSERT INTO KNOWLEDGE VALUES('default/empty','{\"name\":\"Empty\",\"description\":\"\",\"summary\":\"DoNotResurrectToken\"}')", string(legacy), string(legacy)); err != nil {
		t.Fatal(err)
	}
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { errs <- migrateKnowledgeDescriptions(context.Background(), db) }()
	}
	for i := 0; i < 2; i++ {
		if err = <-errs; err != nil {
			t.Fatal(err)
		}
	}
	var data string
	if err = db.QueryRow("SELECT DATA_ FROM KNOWLEDGE WHERE KEY_='default/guide'").Scan(&data); err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err = json.Unmarshal([]byte(data), &record); err != nil {
		t.Fatal(err)
	}
	if record["description"] != description || record["revision"] != float64(7) {
		t.Fatal(record)
	}
	if _, exists := record["summary"]; exists {
		t.Fatal("legacy key retained")
	}
	var version string
	if err = db.QueryRow("SELECT DATA_ FROM CONFIG_VERSION WHERE KEY_='default/guide'").Scan(&version); err != nil || version != string(legacy) {
		t.Fatalf("history modified: %s %v", version, err)
	}
	var count int
	if err = db.QueryRow("SELECT count(*) FROM KNOWLEDGE_SEARCH WHERE KNOWLEDGE_SEARCH MATCH 'MigrationSearchToken'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("old record not indexed: %d %v", count, err)
	}
	if err = db.QueryRow("SELECT count(*) FROM KNOWLEDGE_SEARCH WHERE KNOWLEDGE_SEARCH MATCH 'DoNotResurrectToken'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("explicit empty description was overwritten: %d %v", count, err)
	}
	rows, err := db.Query("PRAGMA table_info(KNOWLEDGE_SEARCH)")
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for rows.Next() {
		var cid, nn, pk int
		var name, kind string
		var def any
		if err = rows.Scan(&cid, &name, &kind, &nn, &def, &pk); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	rows.Close()
	if strings.Join(names, ",") != "KEY_,NAME_,DESCRIPTION_,CONTENT_" {
		t.Fatal(names)
	}
	if _, err = db.Exec(`INSERT INTO KNOWLEDGE VALUES('default/new','{"name":"New","description":"CanonicalSearchToken","content":"Body"}')`); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow("SELECT count(*) FROM KNOWLEDGE_SEARCH WHERE KNOWLEDGE_SEARCH MATCH 'CanonicalSearchToken'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("new description not indexed: %d %v", count, err)
	}
	if _, err = db.Exec(`UPDATE KNOWLEDGE SET DATA_='{"name":"New","description":"UpdatedSearchToken","content":"Body"}' WHERE KEY_='default/new'`); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow("SELECT count(*) FROM KNOWLEDGE_SEARCH WHERE KNOWLEDGE_SEARCH MATCH 'CanonicalSearchToken'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("stale index: %d %v", count, err)
	}
	if _, err = db.Exec("DELETE FROM KNOWLEDGE WHERE KEY_='default/new'"); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow("SELECT count(*) FROM KNOWLEDGE_SEARCH WHERE KNOWLEDGE_SEARCH MATCH 'UpdatedSearchToken'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("deleted record indexed: %d %v", count, err)
	}
}

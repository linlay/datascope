package management

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

const knowledgeSearchDDL = `CREATE VIRTUAL TABLE IF NOT EXISTS KNOWLEDGE_SEARCH USING fts5(KEY_ UNINDEXED,NAME_,DESCRIPTION_,CONTENT_,tokenize='trigram');`
const knowledgeTriggerDDL = `
CREATE TRIGGER TR_KNOWLEDGE_INSERT AFTER INSERT ON KNOWLEDGE BEGIN INSERT INTO KNOWLEDGE_SEARCH VALUES(new.KEY_,json_extract(new.DATA_,'$.name'),coalesce(json_extract(new.DATA_,'$.description'),json_extract(new.DATA_,'$.summary'),''),json_extract(new.DATA_,'$.content'));END;
CREATE TRIGGER TR_KNOWLEDGE_UPDATE AFTER UPDATE ON KNOWLEDGE BEGIN DELETE FROM KNOWLEDGE_SEARCH WHERE KEY_=old.KEY_;INSERT INTO KNOWLEDGE_SEARCH VALUES(new.KEY_,json_extract(new.DATA_,'$.name'),coalesce(json_extract(new.DATA_,'$.description'),json_extract(new.DATA_,'$.summary'),''),json_extract(new.DATA_,'$.content'));END;
CREATE TRIGGER TR_KNOWLEDGE_DELETE AFTER DELETE ON KNOWLEDGE BEGIN DELETE FROM KNOWLEDGE_SEARCH WHERE KEY_=old.KEY_;END;`

func canonicalKnowledgeJSON(raw string) (string, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return "", err
	}
	if fields == nil {
		return "", errors.New("知识记录必须是 JSON 对象")
	}
	var values struct {
		Description *string `json:"description"`
		Summary     *string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return "", err
	}
	description := ""
	if values.Description != nil {
		description = *values.Description
	} else if values.Summary != nil {
		description = *values.Summary
	}
	fields["description"], _ = json.Marshal(description)
	delete(fields, "summary")
	data, err := json.Marshal(fields)
	return string(data), err
}

// Both server processes can start together. An immediate SQLite transaction
// serializes the record conversion and derived-index replacement.
func migrateKnowledgeDescriptions(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "PRAGMA table_info(KNOWLEDGE_SEARCH)")
	if err != nil {
		return err
	}
	hasDescription := false
	for rows.Next() {
		var cid, notnull, pk int
		var name, kind string
		var defaultValue any
		if err = rows.Scan(&cid, &name, &kind, &notnull, &defaultValue, &pk); err != nil {
			rows.Close()
			return err
		}
		hasDescription = hasDescription || name == "DESCRIPTION_"
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DROP TRIGGER IF EXISTS TR_KNOWLEDGE_INSERT;DROP TRIGGER IF EXISTS TR_KNOWLEDGE_UPDATE;DROP TRIGGER IF EXISTS TR_KNOWLEDGE_DELETE;`); err != nil {
		return err
	}
	if !hasDescription {
		if _, err = tx.ExecContext(ctx, "DROP TABLE IF EXISTS KNOWLEDGE_SEARCH"); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, knowledgeSearchDDL+knowledgeTriggerDDL); err != nil {
		return err
	}
	rows, err = tx.QueryContext(ctx, `SELECT KEY_,DATA_ FROM KNOWLEDGE WHERE json_type(DATA_,'$.summary') IS NOT NULL OR json_type(DATA_,'$.description') IS NULL OR json_type(DATA_,'$.description')='null'`)
	if err != nil {
		return err
	}
	records := []Record{}
	for rows.Next() {
		var r Record
		if err = rows.Scan(&r.Key, &r.Data); err != nil {
			rows.Close()
			return err
		}
		records = append(records, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, r := range records {
		data, e := canonicalKnowledgeJSON(r.Data)
		if e != nil {
			return e
		}
		if _, err = tx.ExecContext(ctx, "UPDATE KNOWLEDGE SET DATA_=? WHERE KEY_=?", data, r.Key); err != nil {
			return err
		}
	}
	if !hasDescription {
		if _, err = tx.ExecContext(ctx, `DELETE FROM KNOWLEDGE_SEARCH;INSERT INTO KNOWLEDGE_SEARCH SELECT KEY_,json_extract(DATA_,'$.name'),coalesce(json_extract(DATA_,'$.description'),json_extract(DATA_,'$.summary'),''),json_extract(DATA_,'$.content') FROM KNOWLEDGE;`); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO SCHEMA_VERSION VALUES(2,CURRENT_TIMESTAMP)"); err != nil {
		return err
	}
	return tx.Commit()
}

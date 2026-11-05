package management

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestSQLExamplesSplitAndLegacyRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "management")
	state := emptyState()
	state.Database = map[string]any{"id": "securities_demo", "revision": float64(7), "sql_examples": []any{map[string]any{"label": "Demo", "sql": "SELECT 1"}}}
	if err := publishState(path, state); err != nil {
		t.Fatal(err)
	}
	database, err := os.ReadFile(filepath.Join(path, "database.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(database, []byte("sqlExamples")) {
		t.Fatal("examples embedded in database.yml")
	}
	examples, err := os.ReadFile(filepath.Join(path, "sql-examples.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(examples, []byte("SELECT 1")) {
		t.Fatal("examples missing")
	}
	loaded, err := readState(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Database["sql_examples"].([]any)) != 1 {
		t.Fatal("split examples not loaded")
	}
	// Construct an old generation and verify the next publication moves its examples.
	inline, err := marshalDocument(map[string]any{"database": state.Database})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(path, "sql-examples.yml")); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(path, "database.yml"), inline, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err = readState(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = publishState(path, loaded); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(path, "sql-examples.yml")); err != nil {
		t.Fatal(err)
	}
	if loaded.Database["revision"] != float64(7) {
		t.Fatal("split changed revision")
	}
	// Explicit clearing is authoritative and must not resurrect old examples.
	if err = os.WriteFile(filepath.Join(path, "sql-examples.yml"), []byte("sqlExamples: []\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cleared, err := readState(path)
	if err != nil {
		t.Fatal(err)
	}
	if examples, ok := cleared.Database["sql_examples"]; ok && len(examples.([]any)) != 0 {
		t.Fatal("cleared examples restored")
	}
	// Reject two competing configuration sources rather than silently losing edits.
	if err = os.WriteFile(filepath.Join(path, "database.yml"), inline, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = readState(path); err == nil {
		t.Fatal("duplicate example sources accepted")
	}
}

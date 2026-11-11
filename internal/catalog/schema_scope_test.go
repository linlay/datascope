package catalog

import (
	"context"
	"datascope-server/internal/management"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"datascope-server/internal/rewrite"
	"datascope-server/internal/store"
)

func TestWriterFollowsSchemaScopeEdit(t *testing.T) {
	for _, scope := range []struct {
		name    string
		schema  string
		allowed []string
	}{
		{"change default", "sales", []string{"sales"}},
		{"multiple schemas", "", []string{"public", "sales"}},
		{"empty scope", "", []string{}},
		{"legacy scope", "public", nil},
	} {
		t.Run(scope.name, func(t *testing.T) {
			read := &store.Connection{Host: "localhost", Port: 5432, Database: "business", Schema: "public", AllowedSchemas: []string{"public"}, Username: "reader", PasswordEnv: "DATASCOPE_DB_READ", TLSMode: "require"}
			writer := *read
			writer.Username, writer.PasswordEnv = "writer", "DATASCOPE_DB_WRITE"
			old := Database{Driver: "postgresql", Connection: read, WriteConnection: &writer, WriteEnabled: true}
			next := old
			connection := *read
			connection.Schema, connection.AllowedSchemas = scope.schema, scope.allowed
			next.Connection = &connection
			syncWriterSchemaScope(&next, old)
			if err := validateActions(next); err != nil {
				t.Fatal(err)
			}
			if next.WriteConnection.Schema != scope.schema || !reflect.DeepEqual(next.WriteConnection.AllowedSchemas, scope.allowed) {
				t.Fatal("writer retained stale scope")
			}
			if next.WriteConnection.Username != "writer" || next.WriteConnection.PasswordEnv != "DATASCOPE_DB_WRITE" || old.WriteConnection.Schema != "public" || !reflect.DeepEqual(old.WriteConnection.AllowedSchemas, []string{"public"}) {
				t.Fatal("scope edit changed credentials or saved record")
			}
			// Explicit target changes cannot be hidden by synchronization.
			for _, field := range []string{"host", "database", "schema", "tls"} {
				bad := next
				changed := *next.WriteConnection
				switch field {
				case "host":
					changed.Host = "other"
				case "database":
					changed.Database = "other"
				case "schema":
					changed.Schema = "other"
					changed.AllowedSchemas = nil
				case "tls":
					changed.TLSMode = "disable"
				}
				bad.WriteConnection = &changed
				syncWriterSchemaScope(&bad, old)
				if err := validateActions(bad); err == nil {
					t.Fatalf("accepted mismatched %s", field)
				}
			}
		})
	}
}

func TestSchemaIdentityMigrationPreservesAccessAnnotationsAndMasking(t *testing.T) {
	yes := true
	d := Database{Driver: "oracle", Connection: &store.Connection{Schema: "SALES"}, Config: &rewrite.Config{Tables: []*rewrite.Policy{{Table: "records", Enabled: &yes, Columns: []string{"ID"}, SQL: `SELECT * FROM main.records WHERE note='main.records' AND id=${userId} AND EXISTS(SELECT 1 FROM main.other)`}}}, Discovery: &Discovery{Tables: []store.TableSchema{{Name: "RECORDS", Columns: []store.Column{{Name: "ID", Masking: &store.Masking{Type: "full"}}}}}, Annotations: map[string]store.TableAnnotation{"RECORDS": {Annotation: store.Annotation{DisplayName: "记录"}}}}}
	migrateSchemaIdentity(&d)
	if d.SchemaIdentityVersion != 1 || strings.Join(d.Connection.AllowedSchemas, ",") != "SALES" || d.Config.Tables[0].Table != "SALES.RECORDS" || d.Discovery.Tables[0].Name != "SALES.RECORDS" {
		t.Fatalf("identity migration failed: %+v", d)
	}
	if d.Discovery.Tables[0].Columns[0].Masking.Type != "full" || d.Discovery.Annotations["SALES.RECORDS"].DisplayName != "记录" || !*d.Config.Tables[0].Enabled {
		t.Fatal("managed permissions lost")
	}
	sql := d.Config.Tables[0].SQL
	if !strings.Contains(sql, `FROM "SALES".records`) || !strings.Contains(sql, `FROM "SALES".other`) || !strings.Contains(sql, `note='main.records'`) || !strings.Contains(sql, `${userId}`) {
		t.Fatalf("template migration changed semantics: %s", sql)
	}
	before, _ := json.Marshal(d)
	migrateSchemaIdentity(&d)
	after, _ := json.Marshal(d)
	if string(before) != string(after) {
		t.Fatal("migration not idempotent")
	}
}
func TestSchemaScopeRemovalDoesNotTransferSameNamePolicy(t *testing.T) {
	yes, no := true, false
	d := Database{Connection: &store.Connection{AllowedSchemas: []string{"B"}}, Config: &rewrite.Config{Tables: []*rewrite.Policy{{Table: "A.records", Enabled: &yes}, {Table: "b.records", Enabled: &no}}}, Discovery: &Discovery{Tables: []store.TableSchema{{Name: "A.records"}, {Name: "B.records"}}}}
	pruneSchemaScope(&d)
	if len(d.Config.Tables) != 1 || d.Config.Tables[0].Table != "b.records" || *d.Config.Tables[0].Enabled || len(d.Discovery.Tables) != 1 || d.Discovery.Tables[0].Name != "B.records" {
		t.Fatal("scope removal expanded same-name policy")
	}
}

func TestSchemaIdentityMigrationPersistsAndIsRestartSafe(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "management.db")
	m, err := management.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	d := Database{ID: "legacy", Name: "Legacy", Driver: "oracle", Revision: 4, Connection: &store.Connection{Schema: "A"}, Config: &rewrite.Config{Version: 1, Tables: []*rewrite.Policy{{Table: "RECORDS"}}}, Discovery: &Discovery{Tables: []store.TableSchema{{Name: "RECORDS", Columns: []store.Column{{Name: "ID", Masking: &store.Masking{Type: "full"}}}}}, Annotations: map[string]store.TableAnnotation{}}}
	raw, _ := json.Marshal(d)
	if err := m.UpdateRecords(ctx, "databases", "fixture", "", func([]management.Record) ([]management.Record, error) {
		return []management.Record{{Key: d.ID, Data: string(raw)}}, nil
	}); err != nil {
		t.Fatal(err)
	}
	c := &Catalog{Management: m}
	if err := c.MigrateSchemaIdentities(ctx); err != nil {
		t.Fatal(err)
	}
	m.Close()
	reopened, err := management.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	c.Management = reopened
	if err := c.MigrateSchemaIdentities(ctx); err != nil {
		t.Fatal(err)
	}
	records, err := reopened.Records(ctx, "databases")
	if err != nil {
		t.Fatal(err)
	}
	var next Database
	if err := json.Unmarshal([]byte(records[0].Data), &next); err != nil {
		t.Fatal(err)
	}
	if next.Revision != 5 || next.SchemaIdentityVersion != 1 || next.Config.Tables[0].Table != "A.RECORDS" || next.Discovery.Tables[0].Name != "A.RECORDS" || next.Discovery.Tables[0].Columns[0].Masking.Type != "full" {
		t.Fatalf("persisted migration invalid: %+v", next)
	}
}

package catalog_test

import (
	"context"
	"database/sql"
	"testing"

	"datascope-server/internal/catalog"
	"datascope-server/internal/store"
)

func TestMaskingSurvivesDiscoveryButNotDeletedFields(t *testing.T) {
	c, path := setup(t)
	ctx := context.Background()
	if err := c.Add(ctx, catalog.Database{ID: "masked", Name: "Masked", Driver: "sqlite", Path: path}); err != nil {
		t.Fatal(err)
	}
	d := mustGet(t, c, "masked")
	for i := range d.Discovery.Tables {
		if d.Discovery.Tables[i].Name == "records" {
			for j := range d.Discovery.Tables[i].Columns {
				col := &d.Discovery.Tables[i].Columns[j]
				if col.Name == "secret" {
					col.Masking = &store.Masking{Type: "full"}
				}
			}
		}
	}
	if err := c.Save(ctx, *d); err != nil {
		t.Fatal(err)
	}
	d = mustGet(t, c, "masked")
	version := d.Discovery.Version
	revision := d.Revision
	next, err := c.Discover(ctx, "masked")
	if err != nil {
		t.Fatal(err)
	}
	if next.Discovery.Pending != nil || next.Revision != revision || next.Discovery.Version != version {
		t.Fatal("masking created a false source change")
	}
	writer, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	for _, statement := range []string{`ALTER TABLE records ADD COLUMN extra TEXT`, `ALTER TABLE records DROP COLUMN secret`, `ALTER TABLE records ADD COLUMN secret TEXT`} {
		if _, err = writer.Exec(statement); err != nil {
			t.Fatal(err)
		}
		next, err = c.Discover(ctx, "masked")
		if err != nil {
			t.Fatal(err)
		}
		if next.Discovery.Pending == nil {
			t.Fatal("missing pending source change")
		}
		// Pending source metadata must not contain managed rules.
		for _, table := range next.Discovery.Pending.Tables {
			for _, col := range table.Columns {
				if col.Masking != nil {
					t.Fatal("managed rule leaked into source snapshot")
				}
			}
		}
		next, err = c.ApplyDiscovery(ctx, "masked", next.Discovery.Pending.ID, next.Revision, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, table := range next.Discovery.Tables {
			if table.Name == "records" {
				for _, col := range table.Columns {
					if col.Name == "secret" {
						wantRule := statement == `ALTER TABLE records ADD COLUMN extra TEXT`
						if (col.Masking != nil) != wantRule {
							t.Fatalf("rule lifecycle wrong after %s: %+v", statement, col)
						}
					}
				}
			}
		}
	}
}

func TestMaskingRejectsInvalidAndUnknownColumns(t *testing.T) {
	c, path := setup(t)
	ctx := context.Background()
	if err := c.Add(ctx, catalog.Database{ID: "masked", Name: "Masked", Driver: "sqlite", Path: path}); err != nil {
		t.Fatal(err)
	}
	for _, rule := range []*store.Masking{{Type: "unknown"}, {Type: "partial", KeepPrefix: -1}, {Type: "partial", KeepSuffix: 101}} {
		d := mustGet(t, c, "masked")
		d.Discovery.Tables[0].Columns[0].Masking = rule
		if err := c.Save(ctx, *d); err == nil {
			t.Fatal("invalid rule accepted", rule)
		}
	}
	d := mustGet(t, c, "masked")
	d.Discovery.Tables[0].Columns = append(d.Discovery.Tables[0].Columns, store.Column{Name: "made_up", Masking: &store.Masking{Type: "full"}})
	if err := c.Save(ctx, *d); err == nil {
		t.Fatal("unknown field accepted")
	}
}

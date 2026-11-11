package store

import (
	"context"
	"strings"
	"testing"
)

func TestOptionalSchemaAndExplicitScopeConfiguration(t *testing.T) {
	for _, driver := range []string{"mysql", "oceanbase", "postgresql", "oracle", "dameng"} {
		t.Run(driver, func(t *testing.T) {
			c := validConnection(driver)
			c.Schema = ""
			if (driver == "mysql" || driver == "oceanbase") {
				c.Database = ""
			}
			c.AllowedSchemas = []string{"A", "B"}
			if err := ValidateConnection(driver, &c); err != nil {
				t.Fatal(err)
			}
			c.Schema = "A"
			if (driver == "mysql" || driver == "oceanbase") {
				c.Database = "A"
			}
			if err := ValidateConnection(driver, &c); err != nil {
				t.Fatal(err)
			}
			c.AllowedSchemas = []string{"B"}
			if err := ValidateConnection(driver, &c); err == nil {
				t.Fatal("default outside whitelist accepted")
			}
			c.AllowedSchemas = []string{"A", "A"}
			if err := ValidateConnection(driver, &c); err == nil {
				t.Fatal("duplicate namespace accepted")
			}
		})
	}
	c := validConnection("oracle")
	c.Schema = ""
	c.ServiceName = ""
	if err := ValidateConnection("oracle", &c); err == nil {
		t.Fatal("service must remain mandatory")
	}
}
func TestSameNameTablesKeepIndependentMaskingAcrossScopes(t *testing.T) {
	st, _ := NewOracleSchemaFixture([]string{"A", "B"})
	defer st.Close()
	if _, err := st.TableNames(context.Background()); err != nil {
		t.Fatal(err)
	}
	st.RegisteredSchema = []TableSchema{{Name: "A.RECORDS", Columns: []Column{{Name: "VALUE", Masking: &Masking{Type: "full"}}}}, {Name: "B.RECORDS", Columns: []Column{{Name: "VALUE", Masking: &Masking{Type: "partial", KeepPrefix: 1}}}}}
	for _, query := range []string{`SELECT a.VALUE AS av,b.VALUE AS bv FROM A.RECORDS a JOIN B.RECORDS b ON a.VALUE=b.VALUE`, `WITH x AS(SELECT VALUE FROM A.RECORDS) SELECT x.VALUE AS av,b.VALUE AS bv FROM x JOIN B.RECORDS b ON x.VALUE=b.VALUE`, `SELECT x.VALUE AS av,b.VALUE AS bv FROM (SELECT VALUE FROM A.RECORDS) x JOIN B.RECORDS b ON x.VALUE=b.VALUE`} {
		_, plan, err := st.prepareMasking(query)
		if err != nil {
			t.Fatal(err)
		}
		out := &QueryResult{Columns: []string{"av", "bv"}, Rows: []map[string]any{{"av": "Alice", "bv": "Alice"}}}
		if err := st.applyMasking(out, plan); err != nil {
			t.Fatal(err)
		}
		if out.Rows[0]["av"] != "******" || out.Rows[0]["bv"] != "A****" || !strings.HasPrefix(plan[0].domain, "A.RECORDS") || !strings.HasPrefix(plan[1].domain, "B.RECORDS") {
			t.Fatalf("same-name masking rules mixed: %+v %+v", out, plan)
		}
	}
}

func TestOnlyRawTestPathCanResolveUnenrolledMaskingSources(t *testing.T) {
	st, probe := NewOracleSchemaFixture([]string{"A", "B"})
	defer st.Close()
	ctx := context.Background()
	if _, err := st.TableNames(ctx); err != nil {
		t.Fatal(err)
	}
	st.RegisteredSchema = []TableSchema{{Name: "A.RECORDS", Columns: []Column{{Name: "VALUE", Masking: &Masking{Type: "full"}}}}}
	before := probe.ColumnReads
	if _, _, err := st.prepareMaskingContext(ctx, `SELECT VALUE FROM B.RECORDS`, false); err == nil {
		t.Fatal("formal masking path enrolled an unknown table")
	}
	if probe.ColumnReads != before {
		t.Fatal("formal query fetched unenrolled metadata")
	}
	if _, _, err := st.prepareMaskingContext(ctx, `SELECT VALUE FROM B.RECORDS`, true); err != nil {
		t.Fatal(err)
	}
	if probe.ColumnReads <= before {
		t.Fatal("raw test did not inspect its referenced table")
	}
}

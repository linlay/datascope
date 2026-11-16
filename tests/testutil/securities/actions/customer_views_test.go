package demoactions_test

import (
	"context"
	"datascope-server/internal/application"
	"datascope-server/internal/store"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"testing"
)

func TestSecuritiesCustomerViewQueries(t *testing.T) {
	app, ctx := platformFixture(t)
	d, err := app.Catalog.Get("default")
	if err != nil {
		t.Fatal(err)
	}
	svc, err := app.Catalog.Open(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Store.Close()
	raw, err := os.ReadFile("../../../../tests/fixtures/securities/views/securities_queries.json")
	if err != nil {
		t.Fatal(err)
	}
	queries := map[string]string{}
	if err = json.Unmarshal(raw, &queries); err != nil {
		t.Fatal(err)
	}
	all := map[string]any{"arg_customer": "C00000061", "arg_date": "2026-10-20", "arg_start": "2026-10-19", "arg_end": "2026-10-20", "arg_keyword": "", "arg_like": "%", "arg_risk": "", "arg_type": "", "arg_limit": float64(20), "arg_offset": float64(0)}
	params := func(sql string) map[string]any {
		out := map[string]any{}
		for _, name := range regexp.MustCompile(`:arg_[a-z_]+`).FindAllString(sql, -1) {
			out[name[1:]] = all[name[1:]]
		}
		return out
	}
	for _, identity := range []int64{1, 121, 175} {
		for name, sql := range queries {
			t.Run(fmt.Sprintf("%s/%d", name, identity), func(t *testing.T) {
				result, err := svc.QueryWithParameters(ctx, sql, identity, 200, params(sql))
				if err != nil {
					t.Fatal(err, sql)
				}
				if name == "customer" && len(result.Rows) != 1 {
					t.Fatal("expected visible customer")
				}
			})
		}
	}
	// A customer's visible assets must add independent cash/holding aggregates,
	// without multiplying rows by the number of securities or accounts.
	asset, err := svc.QueryWithParameters(ctx, queries["assets"], 1, 200, params(queries["assets"]))
	if err != nil {
		t.Fatal(err)
	}
	cash, err := svc.QueryWithParameters(ctx, queries["cash"], 1, 200, params(queries["cash"]))
	if err != nil {
		t.Fatal(err)
	}
	holdings, err := svc.QueryWithParameters(ctx, queries["holdings"], 1, 200, params(queries["holdings"]))
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]int64{}
	for _, result := range [][]map[string]any{cash.Rows, holdings.Rows} {
		for _, row := range result {
			expected[row["customer_no"].(string)] += row["metric"].(int64)
		}
	}
	for _, row := range asset.Rows {
		if got, want := row["metric"].(int64), expected[row["customer_no"].(string)]; got != want {
			t.Fatalf("asset total %d, want %d", got, want)
		}
	}
	// A manager must not gain another manager's customer through list or detail.
	all["arg_customer"] = "C00000001"
	visible, err := svc.QueryWithParameters(ctx, queries["customer"], 1, 200, params(queries["customer"]))
	if err != nil {
		t.Fatal(err)
	}
	if len(visible.Rows) != 0 {
		t.Fatal("customer detail leaked outside manager scope")
	}
	all["arg_limit"] = float64(1)
	first, err := svc.QueryWithParameters(ctx, queries["customers"], 175, 200, params(queries["customers"]))
	if err != nil {
		t.Fatal(err)
	}
	all["arg_offset"] = float64(1)
	second, err := svc.QueryWithParameters(ctx, queries["customers"], 175, 200, params(queries["customers"]))
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Rows) != 1 || len(second.Rows) != 1 || first.Rows[0]["customer_no"] == second.Rows[0]["customer_no"] {
		t.Fatal("customer pagination repeated rows")
	}
	all["arg_keyword"] = "陈雨桐"
	all["arg_like"] = "%陈雨桐%"
	all["arg_offset"] = float64(0)
	filtered, err := svc.QueryWithParameters(ctx, queries["customers"], 175, 200, params(queries["customers"]))
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Rows) != 1 || filtered.Rows[0]["name"] != "陈雨桐" {
		t.Fatal("customer search failed")
	}
	all["arg_risk"] = "C5"
	empty, err := svc.QueryWithParameters(ctx, queries["customer_count"], 175, 200, params(queries["customer_count"]))
	if err != nil {
		t.Fatal(err)
	}
	if empty.Rows[0]["total"] != int64(0) {
		t.Fatal("customer risk filter failed")
	}
}

// Match the shipped securities demo's managed masking rules, rather than
// testing only an unmasked fixture where expression subqueries are allowed.
func maskSecuritiesCustomers(t *testing.T, app *application.Service, ctx context.Context) {
	t.Helper()
	d, err := app.Catalog.Get("default")
	if err != nil {
		t.Fatal(err)
	}
	if d.Discovery == nil {
		d, err = app.Catalog.Discover(ctx, d.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	rules := map[string]*store.Masking{"name": {Type: "partial", KeepPrefix: 1}, "mobile": {Type: "partial", KeepPrefix: 3, KeepSuffix: 4}, "id_no": {Type: "hmac"}, "birth_date": {Type: "omit"}}
	for i := range d.Discovery.Tables {
		if d.Discovery.Tables[i].Name == "customers" {
			for j := range d.Discovery.Tables[i].Columns {
				col := &d.Discovery.Tables[i].Columns[j]
				if rule, ok := rules[col.Name]; ok {
					col.Masking = rule
				}
			}
		}
	}
	if _, err = app.Call(ctx, "save_database", application.Input{DatabaseID: d.ID, Database: d}); err != nil {
		t.Fatal(err)
	}
}

func TestSecuritiesCustomerQueriesWithMasking(t *testing.T) {
	app, ctx := platformFixture(t)
	maskSecuritiesCustomers(t, app, ctx)
	d, err := app.Catalog.Get("default")
	if err != nil {
		t.Fatal(err)
	}
	svc, err := app.Catalog.Open(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Store.Close()
	raw, err := os.ReadFile("../../../../tests/fixtures/securities/views/securities_queries.json")
	if err != nil {
		t.Fatal(err)
	}
	queries := map[string]string{}
	if err = json.Unmarshal(raw, &queries); err != nil {
		t.Fatal(err)
	}
	all := map[string]any{"arg_customer": "C00000061", "arg_date": "2026-10-20", "arg_start": "2026-10-19", "arg_end": "2026-10-20", "arg_keyword": "", "arg_like": "%", "arg_risk": "", "arg_type": "", "arg_limit": float64(20), "arg_offset": float64(0)}
	for _, identity := range []int64{1, 175} {
		for name, sql := range queries {
			t.Run(fmt.Sprintf("%s/%d", name, identity), func(t *testing.T) {
				binds := map[string]any{}
				for _, key := range regexp.MustCompile(`:arg_[a-z_]+`).FindAllString(sql, -1) {
					binds[key[1:]] = all[key[1:]]
				}
				result, err := svc.QueryWithParameters(ctx, sql, identity, 200, binds)
				if err != nil {
					t.Fatal(err)
				}
				if name == "customer" {
					if len(result.Rows) != 1 || result.Rows[0]["name"] == "苏州华辰银行有限公司" {
						t.Fatal("expected masked customer name", result.Rows)
					}
				}
				if name == "customers" && len(result.Rows) == 0 {
					t.Fatal("expected visible customers")
				}
			})
		}
	}
}

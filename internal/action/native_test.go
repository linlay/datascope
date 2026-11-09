package action_test

import (
	"context"
	"strings"
	"testing"

	"datascope-server/internal/action"
)

func TestNativeActionDefaultSessionBindingsAndExplicitOwnership(t *testing.T) {
	r, db := setup(t)
	r.DatabaseID = "current"
	// Action authorization is explicit; it does not inherit query policies.
	r.Rewriter = nil
	d := definition(`function main(params, context) {
 const conn = db.session();
 if(conn !== db.session('current')) throw Error('wrong default');
 const row = conn.queryOne('SELECT id,owner FROM records WHERE id=:id', {id:params.id});
 if(!row || row.owner !== context.userId) throw new HttpError(403, 'not owner');
 const result = conn.update('UPDATE records SET value=:value WHERE id=:id AND owner=:userId', {id:params.id,value:params.value,userId:context.userId});
 if(result.rowsAffected !== 1) throw Error('changed');
 return {row:conn.queryOne('SELECT id,value FROM records WHERE id=:id', {id:params.id}), database:context.databaseId, result};
}`)
	args := map[string]any{"id": 1, "value": "x' OR '1'='1"}
	preview, err := r.Run(context.Background(), d, args, true, nil)
	if err != nil || len(preview.Writes) != 1 {
		t.Fatal(preview, err)
	}
	if _, err = r.Run(context.Background(), d, args, false, preview); err != nil {
		t.Fatal(err)
	}
	var value string
	if err = db.QueryRow("SELECT value FROM records WHERE id=1").Scan(&value); err != nil || value != args["value"] {
		t.Fatal(value, err)
	}
	args["id"] = 2
	if _, err = r.Run(context.Background(), d, args, false, nil); err == nil || !strings.Contains(err.Error(), "not owner") {
		t.Fatal(err)
	}
}

func TestNativeActionSQLAndModuleValidation(t *testing.T) {
	for name, source := range map[string]string{
		"dynamic SQL":    `function main(p){return db.session().query('SELECT '+p.value)}`,
		"no where":       `function main(){db.session().update('DELETE FROM records')}`,
		"DDL":            `function main(){db.session().update('DROP TABLE records')}`,
		"multi SQL":      `function main(){return db.session().query('SELECT 1; SELECT 2')}`,
		"npm":            `const x=require('fs');function main(){return x}`,
		"escape":         `const x=require('../employee');function main(){return x}`,
		"dynamic module": `function main(p){return require(p.value)}`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := definition(source).Validate(); err == nil {
				t.Fatal("accepted invalid native Action")
			}
		})
	}
}

func TestNativeActionBindingsAndResultShapes(t *testing.T) {
	r, _ := setup(t)
	d := definition(`function main(p){
 const conn=db.session();
 const inserted=conn.update('INSERT INTO records(owner,value,amount) VALUES(?,?,?)',[10,p.value,1]);
 const row=conn.queryOne('SELECT id,value,amount FROM records WHERE id=:id OR id=:id',{id:inserted.lastInsertId});
 const rows=conn.query('SELECT id FROM records ORDER BY id');
 if(inserted.rowsAffected!==1 || rows.length!==3 || !Array.isArray(rows) || rows.map(r=>r.id).length!==3 || !contains('value')) throw Error('result shape');
 return {row, absent:conn.queryOne('SELECT id FROM records WHERE id=99'), upper:string.upper('ok'),size:list.size([1,2])};
}`)
	out, err := r.Run(context.Background(), d, map[string]any{"id": 1, "value": "test"}, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	result := out.Result.(map[string]any)
	if result["absent"] != nil || result["upper"] != "OK" || result["size"] != float64(2) || result["row"].(map[string]any)["amount"] != "1" {
		t.Fatal(result)
	}
}

func TestNativeActionFailuresAlwaysRollBack(t *testing.T) {
	for name, tail := range map[string]string{
		"SQL caught":        `try{conn.query('SELECT * FROM absent')}catch(e){}`,
		"other database":    `db.session('other')`,
		"manual commit":     `conn.commit()`,
		"alias":             `const query=conn.query;query('SELECT 1')`,
		"bracket":           `conn['query']('SELECT 1')`,
		"forged connection": `({}).query('SELECT 1')`,
		"missing bind":      `conn.query('SELECT :missing',{})`,
		"object bind":       `conn.query('SELECT :v',{v:{}})`,
		"nonfinite bind":    `conn.query('SELECT :v',{v:NaN})`,
		"unsafe bind":       `conn.query('SELECT :v',{v:9007199254740992})`,
		"context immutable": `context.userId=20`,
		"pending":           `return new Promise(()=>{})`,
		"SQL count":         `for(let i=0;i<50;i++)conn.query('SELECT 1')`,
	} {
		t.Run(name, func(t *testing.T) {
			r, db := setup(t)
			d := definition(`function main(params,context){const conn=db.session();conn.update("UPDATE records SET value='changed' WHERE id=1");` + tail + `;return null}`)
			if _, err := r.Run(context.Background(), d, map[string]any{"id": 1, "value": "v"}, false, nil); err == nil {
				t.Fatal("expected failure")
			}
			var value string
			if err := db.QueryRow("SELECT value FROM records WHERE id=1").Scan(&value); err != nil || value != "old" {
				t.Fatal("failed Action committed", value, err)
			}
		})
	}
}

func TestNativeCommonJSModulesShareTransactionAndStayFresh(t *testing.T) {
	r, _ := setup(t)
	r.Libraries = []action.Library{
		{Name: "employee", Title: "Employee", Script: `let n=0;exports.next=()=>++n;exports.save=function(p){return db.session().update('UPDATE records SET value=:value WHERE id=:id',{id:p.id,value:p.value})}`},
		{Name: "unused", Title: "Unused", Script: "invalid JavaScript"},
	}
	d := definition(`const helper=require('./employee.js');function main(p){helper.save(p);return {n:helper.next(),value:db.session().queryOne('SELECT value FROM records WHERE id=:id',{id:p.id}).value}}`)
	queries, err := d.StaticSQL(r.Libraries...)
	if err != nil || len(queries) != 2 {
		t.Fatal(queries, err)
	}
	for i := 0; i < 2; i++ {
		out, err := r.Run(context.Background(), d, map[string]any{"id": 1, "value": "new"}, true, nil)
		if err != nil {
			t.Fatal(err)
		}
		if result := out.Result.(map[string]any); result["n"] != float64(1) || result["value"] != "new" {
			t.Fatal(result)
		}
	}
	r.Libraries[0].Script = `try{db.session().update("UPDATE records SET value='bad' WHERE id=1")}catch(e){}exports.save=()=>null;exports.next=()=>2;`
	if _, err := r.Run(context.Background(), d, map[string]any{"id": 1, "value": "new"}, true, nil); err == nil {
		t.Fatal("module initialization SQL accepted")
	}
}

func TestNativeSettledAsyncAndUndefined(t *testing.T) {
	r, _ := setup(t)
	for _, script := range []string{`async function main(){return 42}`, `function main(){}`} {
		if _, err := r.Run(context.Background(), definition(script), map[string]any{"id": 1, "value": "v"}, true, nil); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNativeOrdinaryMethodsAndCommonJSCycles(t *testing.T) {
	r, _ := setup(t)
	r.Libraries = []action.Library{
		{Name: "a", Title: "A", Script: `exports.value=40;const b=require('./b');exports.answer=()=>b.value+1;`},
		{Name: "b", Title: "B", Script: `exports.value=require('./a').value+1;`},
	}
	d := definition(`const a=require('./a');function main(p){const helper={update(x){return x.value},query(s){return s}};if(helper.query('SELECT 1')!=='SELECT 1'||helper.update(p)!==p.value)throw Error('business method changed');return a.answer()}`)
	if err := action.ValidateLibraries(r.Libraries); err != nil {
		t.Fatal(err)
	}
	out, err := r.Run(context.Background(), d, map[string]any{"id": 1, "value": "new"}, true, nil)
	if err != nil || out.Result != float64(42) {
		t.Fatal(out, err)
	}
}

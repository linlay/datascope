package catalog

import (
	"context"
	"datascope-server/internal/dialect"
	"datascope-server/internal/management"
	"datascope-server/internal/store"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/rqlite/sql"
	"strings"
)

func migrateSchemaIdentity(d *Database) {
	if d.Driver == "sqlite" || d.Connection == nil || d.SchemaIdentityVersion >= 1 {
		return
	}
	if d.Connection.AllowedSchemas == nil && d.Connection.Schema != "" {
		d.Connection.AllowedSchemas = []string{d.Connection.Schema}
	}
	qualify := func(name string) string {
		if strings.Contains(name, ".") || d.Connection.Schema == "" {
			return name
		}
		return d.Connection.Schema + "." + name
	}
	if d.Config != nil {
		for _, p := range d.Config.Tables {
			p.Table = qualify(p.Table)
			p.SQL = qualifyLegacySQL(p.SQL, d.Connection.Schema)
			p.DenyUnless = qualifyLegacySQL(p.DenyUnless, d.Connection.Schema)
		}
	}
	if d.Discovery != nil {
		for i := range d.Discovery.Tables {
			t := &d.Discovery.Tables[i]
			t.Name = qualify(t.Name)
			t.Schema, t.TableName = dialect.SplitTable(t.Name, d.Connection.Schema)
		}
		annotations := map[string]store.TableAnnotation{}
		for k, v := range d.Discovery.Annotations {
			annotations[qualify(k)] = v
		}
		d.Discovery.Annotations = annotations
	}
	// Use the snapshot's physical spelling for persistence. Case-insensitive
	// policy lookup must not create competing YAML files on macOS/Windows.
	if d.Config != nil && d.Discovery != nil {
		physical := map[string]string{}
		for _, t := range d.Discovery.Tables {
			physical[strings.ToLower(t.Name)] = t.Name
		}
		for _, p := range d.Config.Tables {
			if name, ok := physical[strings.ToLower(p.Table)]; ok {
				p.Table = name
			}
		}
	}
	d.SchemaIdentityVersion = 1
}
func pruneSchemaScope(d *Database) {
	if d.Connection == nil {
		return
	}
	engine := dialect.Engine{Schema: d.Connection.Schema, AllowedSchemas: d.Connection.AllowedSchemas}
	allowed := func(name string) bool {
		owner, _ := dialect.SplitTable(name, d.Connection.Schema)
		return engine.AllowsSchema(owner)
	}
	retained := map[string]bool{}
	if d.Discovery != nil {
		tables := []store.TableSchema{}
		for _, t := range d.Discovery.Tables {
			if allowed(t.Name) {
				retained[strings.ToLower(t.Name)] = true
				tables = append(tables, t)
			}
		}
		d.Discovery.Tables = tables
	}
	if d.Config != nil {
		policies := d.Config.Tables[:0]
		for _, p := range d.Config.Tables {
			if (d.Discovery != nil && retained[strings.ToLower(p.Table)]) || (d.Discovery == nil && allowed(p.Table)) {
				policies = append(policies, p)
			}
		}
		d.Config.Tables = policies
	}
}

type CandidatePage struct {
	Items  []store.Table `json:"items"`
	Total  int           `json:"total"`
	Limit  int           `json:"limit"`
	Offset int           `json:"offset"`
}

func (c *Catalog) Candidates(ctx context.Context, id, query string, limit, offset int) (*CandidatePage, error) {
	d, err := c.Get(id)
	if err != nil {
		return nil, err
	}
	st, err := c.OpenStore(ctx, d)
	if err != nil {
		return nil, err
	}
	defer st.Close()
	tables, err := st.CandidateTables(ctx)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}
	matched := []store.Table{}
	for _, t := range tables {
		if strings.Contains(strings.ToLower(t.Name), strings.ToLower(query)) {
			matched = append(matched, t)
		}
	}
	start := min(offset, len(matched))
	end := min(start+limit, len(matched))
	return &CandidatePage{Items: matched[start:end], Total: len(matched), Limit: limit, Offset: offset}, nil
}

// Enrollment is separate from publication; every newly enrolled table is denied.
func (c *Catalog) EnrollTables(ctx context.Context, id string, revision int, names []string) (*Database, error) {
	c.scanMu.Lock()
	defer c.scanMu.Unlock()
	d, err := c.Get(id)
	if err != nil {
		return nil, err
	}
	if d.Revision != revision {
		return nil, errors.New("配置已更新，请刷新后重试")
	}
	if d.Config == nil || len(d.Config.Schemes) == 0 {
		return nil, errors.New("数据库缺少有效策略方案")
	}
	st, err := c.OpenStore(ctx, d)
	if err != nil {
		return nil, err
	}
	defer st.Close()
	known, err := st.TableNames(ctx)
	if err != nil {
		return nil, err
	}
	available := map[string]bool{}
	for _, n := range known {
		available[n] = true
	}
	selected := map[string]bool{}
	for _, n := range names {
		if !available[n] {
			return nil, errors.New("所选表不在允许的候选范围内")
		}
		selected[n] = true
	}
	tables := []store.TableSchema{}
	for _, name := range known {
		if !selected[name] {
			continue
		}
		schema, err := st.Describe(ctx, name)
		if err != nil {
			return nil, err
		}
		tables = append(tables, *schema)
	}
	err = c.updateContext(ctx, "database.tables_enrolled", id, func(items *[]Database) error {
		for i := range *items {
			current := &(*items)[i]
			if current.ID != id {
				continue
			}
			if current.Revision != revision || current.ETag != d.ETag {
				return errors.New("登记期间配置已更新，请重试")
			}
			applyDiscovery(current, tables)
			current.Revision++
			return nil
		}
		return errors.New("数据库不存在")
	})
	if err != nil {
		return nil, err
	}
	return c.Get(id)
}

// Rewrite identifier tokens only: quoted literals and comments must survive a
// migration unchanged. Legacy main.table was a logical alias for the old default.
func qualifyLegacySQL(query, schema string) string {
	if schema == "" {
		return query
	}
	type part struct {
		pos  int
		kind sql.Token
		name string
	}
	scanner := sql.NewScanner(strings.NewReader(query))
	tokens := []part{}
	for {
		pos, kind, name := scanner.Scan()
		if kind == sql.EOF {
			break
		}
		if kind != sql.COMMENT {
			tokens = append(tokens, part{pos.Offset, kind, name})
		}
	}
	src := []rune(query)
	for i := len(tokens) - 3; i >= 0; i-- {
		t := tokens[i]
		next := tokens[i+2]
		ident := func(k sql.Token) bool { return k == sql.IDENT || k == sql.QIDENT || k == sql.BIDENT }
		if !ident(t.kind) || !strings.EqualFold(t.name, "main") || tokens[i+1].kind != sql.DOT || !ident(next.kind) || strings.Contains(next.name, ".") {
			continue
		}
		end := t.pos + len([]rune(t.name))
		if t.kind == sql.QIDENT || t.kind == sql.BIDENT {
			end += 2
		}
		replacement := []rune(`"` + strings.ReplaceAll(schema, `"`, `""`) + `"`)
		src = append(append(append([]rune{}, src[:t.pos]...), replacement...), src[end:]...)
	}
	return string(src)
}

// Persist schema identity upgrades atomically through the existing management
// store, including annotations, field masking and default-only legacy scope.
func (c *Catalog) MigrateSchemaIdentities(ctx context.Context) error {
	if c.Management == nil {
		return nil
	}
	records, err := c.Management.Records(ctx, "databases")
	if err != nil {
		return err
	}
	needed := false
	for _, r := range records {
		var d Database
		if err := json.Unmarshal([]byte(r.Data), &d); err != nil {
			return err
		}
		if d.Driver != "sqlite" && d.Connection != nil && d.SchemaIdentityVersion < 1 {
			needed = true
		}
	}
	if !needed {
		return nil
	}
	return c.Management.UpdateRecords(ctx, "databases", "database.schema_identity_migrated", "", func(records []management.Record) ([]management.Record, error) {
		records = append([]management.Record{}, records...)
		for i := range records {
			var d Database
			if err := json.Unmarshal([]byte(records[i].Data), &d); err != nil {
				return nil, err
			}
			if d.Driver == "sqlite" || d.Connection == nil || d.SchemaIdentityVersion >= 1 {
				continue
			}
			migrateSchemaIdentity(&d)
			d.Revision++
			raw, err := json.Marshal(d)
			if err != nil {
				return nil, fmt.Errorf("迁移表标识: %w", err)
			}
			records[i].Data = string(raw)
		}
		return records, nil
	})
}

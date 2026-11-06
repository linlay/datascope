package store

import (
	"context"
	"database/sql"
	"datascope-server/internal/dialect"
	"fmt"
	"strings"
)

// Candidate discovery reads names only; comments and columns are fetched only
// after explicit enrollment. Metadata visibility alone is not a SELECT grant.
func (s *Store) remoteTables(ctx context.Context) ([]Table, error) {
	return s.remoteTableCandidates(ctx, false)
}
func (s *Store) remoteTableCandidates(ctx context.Context, all bool) ([]Table, error) {
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	query := ""
	switch s.Driver() {
	case "mysql":
		query = `SELECT TABLE_SCHEMA,TABLE_NAME FROM information_schema.TABLES WHERE TABLE_TYPE='BASE TABLE' ORDER BY TABLE_SCHEMA,TABLE_NAME`
	case "postgresql":
		query = `SELECT n.nspname,c.relname FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE c.relkind IN ('r','p') AND n.nspname NOT LIKE 'pg_%' AND n.nspname <> 'information_schema' AND has_table_privilege(c.oid,'SELECT') ORDER BY n.nspname,c.relname`
	default:
		query = `SELECT OWNER,TABLE_NAME FROM ALL_TABLES ORDER BY OWNER,TABLE_NAME`
	}
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tables := []Table{}
	for rows.Next() {
		var owner, table string
		if err := rows.Scan(&owner, &table); err != nil {
			return nil, err
		}
		// Dot-containing identifiers cannot be represented unambiguously by the
		// public schema.table contract, so fail closed instead of merging identities.
		if strings.Contains(owner, ".") || strings.Contains(table, ".") {
			continue
		}
		if !all && !s.engine.AllowsSchema(owner) {
			continue
		}
		tables = append(tables, Table{Name: owner + "." + table, Schema: owner, TableName: table, RowCountEstimated: true})
	}
	return tables, rows.Err()
}
func (s *Store) AvailableSchemas(ctx context.Context) ([]string, error) {
	if s.engine == nil {
		return []string{"main"}, nil
	}
	tables, err := s.remoteTableCandidates(ctx, true)
	if err != nil {
		return nil, err
	}
	names := []string{}
	seen := map[string]bool{}
	for _, t := range tables {
		if !seen[t.Schema] {
			seen[t.Schema] = true
			names = append(names, t.Schema)
		}
	}
	return names, nil
}
func (s *Store) DefaultSchema() string {
	if s.engine == nil {
		return "main"
	}
	return s.engine.Schema
}

func (s *Store) remoteDescribe(ctx context.Context, table string) (*TableSchema, error) {
	physical, err := s.engine.Resolve("", table)
	if strings.Contains(table, ".") {
		owner, name := dialect.SplitTable(table, "")
		physical, err = s.engine.Resolve(owner, name)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrTableNotFound, table)
	}
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	owner, name := dialect.SplitTable(physical, s.engine.Schema)
	// Check actual SELECT permission without fetching business rows.
	tx, err := s.beginReadOnly(ctx)
	if err != nil {
		return nil, err
	}
	readable, e := tx.QueryContext(ctx, "SELECT * FROM "+s.engine.Table(physical)+" WHERE 1=0")
	if e != nil {
		tx.Rollback()
		return nil, fmt.Errorf("表 %s 无读取权限: %w", physical, e)
	}
	readable.Close()
	tx.Rollback()
	out := &TableSchema{Name: physical, Schema: owner, TableName: name, Columns: []Column{}, Indexes: []string{}}
	query := ""
	switch s.Driver() {
	case "mysql":
		query = `SELECT COLUMN_NAME,COLUMN_TYPE,IS_NULLABLE,COLUMN_DEFAULT,COLUMN_COMMENT,CASE WHEN COLUMN_KEY='PRI' THEN 1 ELSE 0 END FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=? AND TABLE_NAME=? ORDER BY ORDINAL_POSITION`
	case "postgresql":
		query = `SELECT a.attname,pg_catalog.format_type(a.atttypid,a.atttypmod),CASE WHEN a.attnotnull THEN 'NO' ELSE 'YES' END,pg_catalog.pg_get_expr(d.adbin,d.adrelid),pg_catalog.col_description(c.oid,a.attnum),CASE WHEN EXISTS(SELECT 1 FROM pg_catalog.pg_index i WHERE i.indrelid=c.oid AND i.indisprimary AND a.attnum=ANY(i.indkey)) THEN 1 ELSE 0 END FROM pg_catalog.pg_attribute a JOIN pg_catalog.pg_class c ON c.oid=a.attrelid JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace LEFT JOIN pg_catalog.pg_attrdef d ON d.adrelid=c.oid AND d.adnum=a.attnum WHERE n.nspname=$1 AND c.relname=$2 AND a.attnum>0 AND NOT a.attisdropped ORDER BY a.attnum`
	default:
		query = `SELECT c.COLUMN_NAME,c.DATA_TYPE,CASE WHEN c.NULLABLE='N' THEN 'NO' ELSE 'YES' END,c.DATA_DEFAULT,m.COMMENTS,CASE WHEN EXISTS(SELECT 1 FROM ALL_CONSTRAINTS p JOIN ALL_CONS_COLUMNS k ON k.OWNER=p.OWNER AND k.CONSTRAINT_NAME=p.CONSTRAINT_NAME WHERE p.OWNER=c.OWNER AND p.TABLE_NAME=c.TABLE_NAME AND p.CONSTRAINT_TYPE='P' AND k.COLUMN_NAME=c.COLUMN_NAME) THEN 1 ELSE 0 END FROM ALL_TAB_COLUMNS c LEFT JOIN ALL_COL_COMMENTS m ON m.OWNER=c.OWNER AND m.TABLE_NAME=c.TABLE_NAME AND m.COLUMN_NAME=c.COLUMN_NAME WHERE c.OWNER=:1 AND c.TABLE_NAME=:2 ORDER BY c.COLUMN_ID`
	}
	rows, err := s.db.QueryContext(ctx, query, owner, name)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var c Column
		var nullable string
		var def, comment sql.NullString
		var primary int
		if err := rows.Scan(&c.Name, &c.Type, &nullable, &def, &comment, &primary); err != nil {
			rows.Close()
			return nil, err
		}
		c.NotNull = nullable == "NO"
		c.PrimaryKey = primary != 0
		c.Comment = comment.String
		if def.Valid {
			value := def.String
			c.Default = &value
		}
		out.Columns = append(out.Columns, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(out.Columns) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrTableNotFound, table)
	}
	switch s.Driver() {
	case "mysql":
		query = `SELECT DISTINCT INDEX_NAME FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=? AND TABLE_NAME=? ORDER BY INDEX_NAME`
	case "postgresql":
		query = `SELECT indexname FROM pg_catalog.pg_indexes WHERE schemaname=$1 AND tablename=$2 ORDER BY indexname`
	default:
		query = `SELECT INDEX_NAME FROM ALL_INDEXES WHERE TABLE_OWNER=:1 AND TABLE_NAME=:2 ORDER BY INDEX_NAME`
	}
	rows, err = s.db.QueryContext(ctx, query, owner, name)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return nil, err
		}
		out.Indexes = append(out.Indexes, name)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	// Foreign-key metadata is only a navigation hint; it never expands schema
	// scope or grants access to a referenced table.
	switch s.Driver() {
	case "mysql":
		query = `SELECT COLUMN_NAME,CONCAT(REFERENCED_TABLE_SCHEMA,'.',REFERENCED_TABLE_NAME),REFERENCED_COLUMN_NAME FROM information_schema.KEY_COLUMN_USAGE WHERE TABLE_SCHEMA=? AND TABLE_NAME=? AND REFERENCED_TABLE_NAME IS NOT NULL`
	case "postgresql":
		query = `SELECT a.attname,rn.nspname || '.' || rc.relname,ra.attname FROM pg_catalog.pg_constraint f JOIN pg_catalog.pg_class c ON c.oid=f.conrelid JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace JOIN pg_catalog.pg_class rc ON rc.oid=f.confrelid JOIN pg_catalog.pg_namespace rn ON rn.oid=rc.relnamespace JOIN LATERAL unnest(f.conkey,f.confkey) AS k(src,dst) ON true JOIN pg_catalog.pg_attribute a ON a.attrelid=c.oid AND a.attnum=k.src JOIN pg_catalog.pg_attribute ra ON ra.attrelid=rc.oid AND ra.attnum=k.dst WHERE f.contype='f' AND n.nspname=$1 AND c.relname=$2`
	default:
		query = `SELECT k.COLUMN_NAME,r.OWNER || '.' || r.TABLE_NAME,r.COLUMN_NAME FROM ALL_CONSTRAINTS f JOIN ALL_CONS_COLUMNS k ON k.OWNER=f.OWNER AND k.CONSTRAINT_NAME=f.CONSTRAINT_NAME JOIN ALL_CONS_COLUMNS r ON r.OWNER=f.R_OWNER AND r.CONSTRAINT_NAME=f.R_CONSTRAINT_NAME AND r.POSITION=k.POSITION WHERE f.CONSTRAINT_TYPE='R' AND f.OWNER=:1 AND f.TABLE_NAME=:2`
	}
	rows, err = s.db.QueryContext(ctx, query, owner, name)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var col, target, ref string
		if err := rows.Scan(&col, &target, &ref); err != nil {
			rows.Close()
			return nil, err
		}
		for i := range out.Columns {
			if out.Columns[i].Name == col {
				out.Columns[i].References = target + "." + ref
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	switch s.Driver() {
	case "mysql":
		query = `SELECT COALESCE(TABLE_COMMENT,'') FROM information_schema.TABLES WHERE TABLE_SCHEMA=? AND TABLE_NAME=?`
	case "postgresql":
		query = `SELECT COALESCE(pg_catalog.obj_description(c.oid,'pg_class'),'') FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND c.relname=$2`
	default:
		query = `SELECT COMMENTS FROM ALL_TAB_COMMENTS WHERE OWNER=:1 AND TABLE_NAME=:2`
	}
	var tableComment sql.NullString
	if err := s.db.QueryRowContext(ctx, query, owner, name).Scan(&tableComment); err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	out.Comment = tableComment.String

	// A metadata summary, not a vendor's lossless CREATE TABLE export.
	var cols []string
	for _, c := range out.Columns {
		v := s.engine.Quote(c.Name) + " " + c.Type
		if c.NotNull {
			v += " NOT NULL"
		}
		cols = append(cols, v)
	}
	out.DDL = "-- 元数据摘要（不含完整约束、默认值或存储选项）\nCREATE TABLE " + s.engine.Table(physical) + " (\n  " + strings.Join(cols, ",\n  ") + "\n);"
	return out, nil
}

// CandidateTables never reads columns or annotations, including when the store
// already has an enrolled snapshot attached.
func (s *Store) CandidateTables(ctx context.Context) ([]Table, error) { return s.sourceTables(ctx) }

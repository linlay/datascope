package store

import (
	parser "github.com/rqlite/sql"
	"strconv"
	"strings"
)

// Bound only the final result, never aggregate inputs or nested SELECTs. Fetch
// one extra row to distinguish an exact-size result from a truncated result.
func boundedQuery(query string, maxRows int) string {
	node, err := parser.NewParser(strings.NewReader(query)).ParseStatement()
	if err != nil {
		return query
	}
	sel, ok := node.(*parser.SelectStatement)
	if !ok || sel.Values.IsValid() || sel.Compound != nil {
		return query
	} // Keep PRAGMA, VALUES and compound-query syntax unchanged.
	cap := strconv.Itoa(maxRows + 1)
	if sel.LimitExpr != nil {
		limit := sel.LimitExpr
		if sel.OffsetComma.IsValid() {
			limit = sel.OffsetExpr
		} // LIMIT offset, count
		literal, ok := limit.(*parser.NumberLit)
		if !ok {
			return query
		}
		n, err := strconv.ParseUint(literal.Value, 10, 64)
		if err != nil || n <= uint64(maxRows+1) {
			return query
		}
		// Preserve identifier quoting: reprinting the whole AST can turn an
		// unknown SQLite column into a double-quoted string literal (DQS).
		src := []rune(query)
		start, end := literal.ValuePos.Offset, literal.ValuePos.Offset+len([]rune(literal.Value))
		if start < 0 || end > len(src) || string(src[start:end]) != literal.Value {
			return query
		}
		return string(src[:start]) + cap + string(src[end:])
	}
	// A newline also terminates a trailing SQL line comment.
	return query + "\nLIMIT " + cap
}

// SetRowLimit describes the transport cap, independent of a SQL LIMIT clause.
func (r *QueryResult) SetRowLimit(requested int) {
	r.EffectiveMaxRows, r.LimitSource = requested, "request"
	if requested <= 0 {
		r.EffectiveMaxRows, r.LimitSource = DefaultMaxRows, "default"
	}
	if requested > HardMaxRows {
		r.EffectiveMaxRows, r.LimitSource = HardMaxRows, "hard_limit"
	}
}

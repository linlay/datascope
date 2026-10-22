package rewrite

import (
	"slices"
	"strings"

	sql "github.com/rqlite/sql"
)

// splice replaces src[start:end] (rune offsets) with text.
type splice struct {
	start, end int
	text       string
}

// addSplice records the in-place replacement of table reference x: the
// span from the table name through its alias and any INDEXED BY / NOT
// INDEXED clause becomes "(subquery) AS alias", or "main.table AS alias"
// for a passthrough table.
func (v *visitor) addSplice(x *sql.QualifiedTableName, table string) {
	aliasIdent := x.Name
	if x.Alias != nil {
		aliasIdent = x.Alias
	}
	start, aliasEnd := x.Name.NamePos.Offset, identEnd(v.src, aliasIdent)
	end := aliasEnd
	if x.Index != nil {
		end = identEnd(v.src, x.Index)
	}
	if x.NotIndexed.IsValid() {
		end = x.NotIndexed.Offset + len("INDEXED")
	}
	if start < 0 || aliasEnd < 0 || end < aliasEnd || end > len(v.src) {
		v.splices = append(v.splices, splice{start: -1}) // spliced falls back
		return
	}
	text := "main." + table
	if sub, ok := v.rewriter.subSQL[table]; ok {
		text = "(" + sub + ")"
	}
	alias := string(v.src[aliasIdent.NamePos.Offset:aliasEnd])
	v.splices = append(v.splices, splice{start: start, end: end, text: text + " AS " + alias})
}

// identEnd returns the rune offset just past id as written in src, or -1.
func identEnd(src []rune, id *sql.Ident) int {
	off := id.NamePos.Offset
	if off < 0 || off >= len(src) {
		return -1
	}
	n := len([]rune(id.Name))
	switch q := src[off]; q {
	case '"', '`':
		n += 2 + strings.Count(id.Name, string(q))
	case '[':
		n += 2
	}
	if off+n > len(src) {
		return -1
	}
	return off + n
}

// spliced applies splices to src. It reports false unless the result parses
// to exactly the statement printed by the AST rewrite, so the text the user
// sees and runs is the statement that was checked.
func spliced(src []rune, splices []splice, printed string) (string, bool) {
	splices = slices.Clone(splices)
	slices.SortFunc(splices, func(a, b splice) int { return a.start - b.start })
	var b strings.Builder
	last := 0
	for _, s := range splices {
		if s.start < last {
			return "", false
		}
		b.WriteString(string(src[last:s.start]))
		b.WriteString(s.text)
		last = s.end
	}
	b.WriteString(string(src[last:]))
	out := b.String()
	stmt, err := sql.NewParser(strings.NewReader(out)).ParseStatement()
	if err != nil || stmt.String() != printed {
		return "", false
	}
	return out, true
}

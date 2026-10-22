package rewrite

import (
	"regexp"
	"strings"

	sql "github.com/rqlite/sql"
)

var bareName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// quotedNames lists, lower case, every identifier written quoted ("x", `x`
// or [x]) in query.
func quotedNames(query string, into map[string]bool) {
	for _, t := range scanTokens(query) {
		if isIdent(t) && t.tok != sql.IDENT {
			into[strings.ToLower(t.lit)] = true
		}
	}
}

// unquoteIdents drops the double quotes the printer puts around every
// identifier where that cannot change the statement: the name is a plain
// word, not a keyword, and not in keep (names the author quoted, since
// SQLite reads an unresolved "x" as the string 'x'). The result must parse
// back to the same statement; otherwise printed is returned unchanged.
func unquoteIdents(printed string, keep map[string]bool) string {
	src := []rune(printed)
	var b strings.Builder
	last := 0
	scanner := sql.NewScanner(strings.NewReader(printed))
	for {
		pos, tok, lit := scanner.Scan()
		if tok == sql.EOF || tok == sql.ILLEGAL {
			break
		}
		if tok != sql.QIDENT || !bareName.MatchString(lit) || sql.Lookup(lit) != sql.IDENT || keep[strings.ToLower(lit)] {
			continue
		}
		b.WriteString(string(src[last:pos.Offset]))
		b.WriteString(lit)
		last = pos.Offset + len(lit) + 2 // ASCII name between two quotes
	}
	b.WriteString(string(src[last:]))
	out := b.String()
	stmt, err := sql.NewParser(strings.NewReader(out)).ParseStatement()
	if err != nil || stmt.String() != printed {
		return printed
	}
	return out
}

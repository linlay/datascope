package dialect

import (
	parser "github.com/rqlite/sql"
	"maps"
	"strings"
)

// A visitor child owns its SELECT's lexical CTE scope. Sibling SELECTs never
// inherit a nested WITH clause. Database resolution remains the final authority.
type scopedVisitor struct {
	scope map[string]bool
	visit func(parser.Node, map[string]bool) (parser.Node, error)
}

func (v *scopedVisitor) Visit(n parser.Node) (parser.Visitor, parser.Node, error) {
	child := *v
	if sel, ok := n.(*parser.SelectStatement); ok {
		child.scope = maps.Clone(v.scope)
		if child.scope == nil {
			child.scope = map[string]bool{}
		}
		if sel.WithClause != nil {
			for _, c := range sel.WithClause.CTEs {
				child.scope[strings.ToLower(c.TableName.Name)] = true
			}
		}
	}
	out, err := child.visit(n, child.scope)
	return &child, out, err
}
func (v *scopedVisitor) VisitEnd(n parser.Node) (parser.Node, error) { return n, nil }

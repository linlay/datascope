package management

import (
	"context"
	"strings"
)

func (s *Store) SearchKnowledge(ctx context.Context, query string) (map[string]bool, error) {
	terms := strings.Fields(query)
	if len(terms) > 20 {
		terms = terms[:20]
	}
	if len(terms) == 0 {
		return nil, nil
	}
	for i, t := range terms {
		terms[i] = "\"" + strings.ReplaceAll(t, "\"", "\"\"") + "\""
	}
	expression := strings.Join(terms, " AND ")
	out := map[string]bool{}
	ids, e := s.sourceIDs()
	if e != nil {
		return nil, e
	}
	for _, id := range ids {
		rows, e := s.sourceDB(id).QueryContext(ctx, "SELECT KEY_ FROM KNOWLEDGE_SEARCH WHERE KNOWLEDGE_SEARCH MATCH ? LIMIT 500", expression)
		if e != nil {
			return nil, e
		}
		for rows.Next() {
			var key string
			if e = rows.Scan(&key); e != nil {
				rows.Close()
				return nil, e
			}
			out[key] = true
		}
		if e = rows.Err(); e != nil {
			rows.Close()
			return nil, e
		}
		rows.Close()
	}
	return out, nil
}

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
	rows, e := s.DB.QueryContext(ctx, "SELECT key FROM knowledge_search WHERE knowledge_search MATCH ? LIMIT 500", expression)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var key string
		if e = rows.Scan(&key); e != nil {
			return nil, e
		}
		out[key] = true
	}
	return out, rows.Err()
}
func (s *Store) initSearch() error {
	_, e := s.DB.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS knowledge_search USING fts5(key UNINDEXED,name,summary,content,tokenize='trigram');
 CREATE TRIGGER IF NOT EXISTS knowledge_search_insert AFTER INSERT ON records WHEN new.bucket='knowledge' BEGIN INSERT INTO knowledge_search VALUES(new.key,json_extract(new.data,'$.name'),json_extract(new.data,'$.summary'),json_extract(new.data,'$.content')); END;
 CREATE TRIGGER IF NOT EXISTS knowledge_search_update AFTER UPDATE ON records WHEN new.bucket='knowledge' BEGIN DELETE FROM knowledge_search WHERE key=old.key; INSERT INTO knowledge_search VALUES(new.key,json_extract(new.data,'$.name'),json_extract(new.data,'$.summary'),json_extract(new.data,'$.content')); END;
 INSERT INTO knowledge_search SELECT key,json_extract(data,'$.name'),json_extract(data,'$.summary'),json_extract(data,'$.content') FROM records WHERE bucket='knowledge' AND key NOT IN(SELECT key FROM knowledge_search);`)
	return e
}

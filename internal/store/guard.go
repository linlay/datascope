package store

import (
	"errors"
	"strings"
)

// ErrStatementNotAllowed is returned for anything other than a single
// SELECT / WITH / VALUES statement. The connection is also opened read-only,
// so this check is a first line of defense, not the only one.
var ErrStatementNotAllowed = errors.New("只允许执行单条只读 SELECT / WITH / VALUES 语句")

var allowedLeadingKeywords = map[string]bool{"SELECT": true, "WITH": true, "VALUES": true}

// NormalizeReadOnlySQL trims trailing semicolons and verifies that sql is a
// single statement starting with an allowed keyword.
func NormalizeReadOnlySQL(sql string) (string, error) {
	body, rest, err := splitFirstStatement(sql)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(stripComments(rest)) != "" {
		return "", errors.New("不支持多条语句")
	}
	keyword := leadingKeyword(body)
	if !allowedLeadingKeywords[keyword] {
		return "", ErrStatementNotAllowed
	}
	return strings.TrimSpace(body), nil
}

// splitFirstStatement returns the text before the first top-level ';' and
// everything after it, skipping quoted identifiers, literals and comments.
func splitFirstStatement(sql string) (string, string, error) {
	for i := 0; i < len(sql); i++ {
		switch c := sql[i]; c {
		case '\'', '"', '`':
			end := strings.IndexByte(sql[i+1:], c)
			if end < 0 {
				return "", "", errors.New("SQL 引号未闭合")
			}
			// Doubled quotes are escapes; the scan simply continues past them.
			i += end + 1
		case '[':
			end := strings.IndexByte(sql[i+1:], ']')
			if end < 0 {
				return "", "", errors.New("SQL 方括号标识符未闭合")
			}
			i += end + 1
		case '-':
			if i+1 < len(sql) && sql[i+1] == '-' {
				end := strings.IndexByte(sql[i:], '\n')
				if end < 0 {
					return sql, "", nil
				}
				i += end
			}
		case '/':
			if i+1 < len(sql) && sql[i+1] == '*' {
				end := strings.Index(sql[i+2:], "*/")
				if end < 0 {
					return "", "", errors.New("SQL 注释未闭合")
				}
				i += end + 3
			}
		case ';':
			// Allow any number of trailing semicolons.
			rest := strings.TrimLeft(sql[i+1:], "; \t\r\n")
			return sql[:i], rest, nil
		}
	}
	return sql, "", nil
}

func stripComments(sql string) string {
	var b strings.Builder
	for i := 0; i < len(sql); i++ {
		if strings.HasPrefix(sql[i:], "--") {
			end := strings.IndexByte(sql[i:], '\n')
			if end < 0 {
				break
			}
			i += end
			continue
		}
		if strings.HasPrefix(sql[i:], "/*") {
			end := strings.Index(sql[i+2:], "*/")
			if end < 0 {
				break
			}
			i += end + 3
			continue
		}
		b.WriteByte(sql[i])
	}
	return b.String()
}

func leadingKeyword(sql string) string {
	s := strings.TrimLeft(stripComments(sql), " \t\r\n(")
	end := strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z')
	})
	if end >= 0 {
		s = s[:end]
	}
	return strings.ToUpper(s)
}

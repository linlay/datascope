// formatSQL adds line breaks and indentation by parenthesis depth so the
// generated permission subqueries are readable. It never changes tokens.
export function formatSQL(sql: string): string {
  const tokens = sql.match(/--[^\r\n]*(?:\r?\n|$)|\/\*[\s\S]*?\*\/|'(?:[^']|'')*'|"(?:[^"]|"")*"|`[^`]*`|\[[^\]]*\]|\s+|[A-Za-z_][A-Za-z0-9_]*|\d+(?:\.\d+)?|./g) ?? [];
  const words = tokens.map((t) => t.toUpperCase());
  const nextWord = (i: number) => {
    for (let j = i + 1; j < words.length; j++) if (!/^\s+$/.test(words[j])) return words[j];
    return '';
  };
  const prevWord = (i: number) => {
    for (let j = i - 1; j >= 0; j--) if (!/^\s+$/.test(words[j])) return words[j];
    return '';
  };
  const always = new Set(['SELECT', 'FROM', 'WHERE', 'HAVING', 'LIMIT', 'UNION', 'EXCEPT', 'INTERSECT', 'AND', 'OR', 'WITH']);
  let depth = 0;
  let out = '';
  tokens.forEach((token, i) => {
    const w = words[i];
    if (token.startsWith('--')) {
      // Keep the newline: flattening a line comment could hide executable SQL.
      out += token.replace(/\r?\n$/, '') + '\n' + '  '.repeat(depth);
      return;
    }
    if (token.startsWith('/*')) {
      out += token;
      return;
    }
    if (/^\s+$/.test(token)) {
      out += ' ';
      return;
    }
    if (token === '(') depth++;
    if (token === ')') depth = Math.max(0, depth - 1);
    const breakHere =
      always.has(w) ||
      (['GROUP', 'ORDER'].includes(w) && nextWord(i) === 'BY') ||
      (['LEFT', 'INNER', 'CROSS', 'NATURAL', 'RIGHT', 'FULL'].includes(w) && ['JOIN', 'OUTER'].includes(nextWord(i))) ||
      (w === 'JOIN' && !['LEFT', 'INNER', 'CROSS', 'NATURAL', 'OUTER', 'RIGHT', 'FULL'].includes(prevWord(i)));
    if (breakHere && out.trim() !== '') {
      out = out.trimEnd() + '\n' + '  '.repeat(depth);
    }
    out += token;
  });
  return out.trim();
}

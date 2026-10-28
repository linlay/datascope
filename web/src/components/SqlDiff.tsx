import { useI18n } from '../ui/preferences';
import { useMemo } from 'react';
type DiffLine = { kind: 'same' | 'removed' | 'added'; text: string };

// Compare the lines as written: the server replaces each table in place with
// a one-line subquery, so every changed line maps to one original line.
export function diffSQL(original: string, rewritten: string): DiffLine[] {
  const a = original.split('\n');
  const b = rewritten.split('\n');
  // Bound memory for large pasted queries; a complete replacement is still valid.
  if (a.length * b.length > 250000) {
    return [...a.map((text): DiffLine => ({ kind: 'removed', text })), ...b.map((text): DiffLine => ({ kind: 'added', text }))];
  }
  const lengths = Array.from({ length: a.length + 1 }, () => new Uint32Array(b.length + 1));
  for (let i = a.length - 1; i >= 0; i--) {
    for (let j = b.length - 1; j >= 0; j--) {
      lengths[i][j] = a[i] === b[j] ? lengths[i + 1][j + 1] + 1 : Math.max(lengths[i + 1][j], lengths[i][j + 1]);
    }
  }
  const lines: DiffLine[] = [];
  let i = 0;
  let j = 0;
  while (i < a.length || j < b.length) {
    if (i < a.length && j < b.length && a[i] === b[j]) {
      lines.push({ kind: 'same', text: a[i++] });
      j++;
    } else if (i < a.length && (j === b.length || lengths[i + 1][j] >= lengths[i][j + 1])) {
      lines.push({ kind: 'removed', text: a[i++] });
    } else lines.push({ kind: 'added', text: b[j++] });
  }
  return lines;
}

export function SqlDiff({ original, rewritten }: { original: string; rewritten: string }) {
 const t=useI18n();
  const lines = useMemo(() => diffSQL(original, rewritten), [original, rewritten]);
  let oldLine = 0;
  let newLine = 0;
  return <div className="sql-diff" role="region" aria-label={t("SQL 改写差异")} tabIndex={0}>
    <div className="sql-diff-heading">{t("− 原始 SQL / + 改写后 SQL · 逐行对比")}</div>
    {lines.every((line) => line.kind === 'same') && <div className="sql-diff-heading">{t("当前权限下 SQL 无变化")}</div>}
    <div className="sql-diff-lines">
      {lines.map((line, index) => <div key={index} className={`sql-diff-line ${line.kind}`}>
        <span className="sql-diff-number">{line.kind !== 'added' ? ++oldLine : ''}</span>
        <span className="sql-diff-number">{line.kind !== 'removed' ? ++newLine : ''}</span>
        <span className="sql-diff-sign">{line.kind === 'added' ? '+' : line.kind === 'removed' ? '−' : ' '}</span>
        <code>{line.text}</code>
      </div>)}
    </div>
  </div>;
}

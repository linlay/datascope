import { useRef, type ReactNode } from 'react';
import { Button, message } from 'antd';
import { CopyOutlined } from '@ant-design/icons';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { useI18n } from '../ui/preferences';

function CopyableCodeBlock({ children }: { children?: ReactNode }) {
 const t = useI18n();
 const codeRef = useRef<HTMLPreElement>(null);
 const copy = async () => {
  const text = codeRef.current?.textContent;
  if (text === undefined || text === null) return;
  try {
   if (navigator.clipboard?.writeText) await navigator.clipboard.writeText(text);
   else {
    // Clipboard API is unavailable on HTTP deployments. Keep the fallback
    // within the block so copying also works inside a modal's focus trap.
    const field = document.createElement('textarea');
    const focused = document.activeElement;
    field.value = text; field.readOnly = true;
    field.style.cssText = 'position:fixed;opacity:0;pointer-events:none';
    codeRef.current!.parentElement!.appendChild(field);
    try { field.select(); if (!document.execCommand('copy')) throw new Error('Copy failed'); }
    finally { field.remove(); if (focused instanceof HTMLElement) focused.focus({ preventScroll: true }); }
   }
   message.success(t('代码已复制'));
  } catch { message.error(t('复制失败，请手动复制')); }
 };
 return <div className="markdown-code-block">
  <pre ref={codeRef}>{children}</pre>
  <Button className="markdown-code-copy" size="small" icon={<CopyOutlined />} aria-label={t('复制代码')} title={t('复制代码')} onClick={() => void copy()}></Button>
 </div>;
}

export default function KnowledgeContent({ content, copyCode = false }: { content: string; copyCode?: boolean }) {
 return <article className="knowledge-markdown"><ReactMarkdown remarkPlugins={[remarkGfm]} skipHtml components={{
  a: ({ children, href }) => <a href={href} target="_blank" rel="noreferrer">{children}</a>,
  ...(copyCode ? { pre: ({ children }: { children?: ReactNode }) => <CopyableCodeBlock>{children}</CopyableCodeBlock> } : {}),
 }}>{content}</ReactMarkdown></article>;
}

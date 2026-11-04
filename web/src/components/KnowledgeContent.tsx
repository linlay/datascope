import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';

export default function KnowledgeContent({ content }: { content: string }) {
 return <article className="knowledge-markdown"><ReactMarkdown remarkPlugins={[remarkGfm]} skipHtml components={{ a: ({ children, href }) => <a href={href} target="_blank" rel="noreferrer">{children}</a> }}>{content}</ReactMarkdown></article>;
}

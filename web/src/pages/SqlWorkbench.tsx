import { useRef, useState } from 'react';
import { Alert, Button, Card, Empty, Input, Segmented, Select, Space, Tag, Typography } from 'antd';
import type { TextAreaRef } from 'antd/es/input/TextArea';
import { FormatPainterOutlined, PlayCircleOutlined, ThunderboltOutlined } from '@ant-design/icons';
import { ApiError, api, type QueryResult, type RewritePreview } from '../api';
import { ResultTable, SchemeTag, SqlBlock, UserPicker } from '../components/shared';
import { SqlDiff } from '../components/SqlDiff';
import { formatSQL } from '../sqlFormat';
import examples from '../sqlExamples.json';

const { Text } = Typography;

interface Outcome<T> {
  data?: T;
  error?: string;
  denied?: boolean;
}

function failure<T>(e: unknown): Outcome<T> {
  const err = e as ApiError;
  return { error: err.message, denied: Boolean(err.body?.denied) };
}

function QueryOutput({ outcome, loading }: { outcome?: Outcome<QueryResult>; loading: boolean }) {
  if (!outcome) return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={loading ? '正在执行…' : '执行后在这里查看结果'} />;
  if (outcome.error) return <Alert type={outcome.denied ? 'warning' : 'error'} showIcon message={outcome.denied ? '权限拒绝' : '执行失败'} description={outcome.error} />;
  const result = outcome.data!;
  return <ResultTable columns={result.columns} rows={result.rows} loading={loading} />;
}

function ResultSummary({ outcome }: { outcome?: Outcome<QueryResult> }) {
  return outcome?.data ? <div className="sql-result-summary">
    <Tag color="blue">{outcome.data.row_count} 行</Tag>
    {outcome.data.truncated && <Text type="warning">结果已截断</Text>}
  </div> : null;
}

export function SqlWorkbench() {
  const [sql, setSql] = useState(examples[1].sql);
  const editor = useRef<TextAreaRef>(null);
  // Execution and formatting always read the live editor; never fall back to
  // a cached state value or a previous rewrite when the editor is unavailable.
  const readSql = () => editor.current?.resizableTextArea?.textArea?.value ?? '';
  const [userId, setUserId] = useState(1);
  const [maxRows, setMaxRows] = useState(200);
  const [action, setAction] = useState<'raw' | 'preview' | 'execute'>();
  const busy = useRef(false);
  const [raw, setRaw] = useState<Outcome<QueryResult>>();
  const [rewritten, setRewritten] = useState<Outcome<RewritePreview>>();
  const [result, setResult] = useState<Outcome<QueryResult>>();
  const [view, setView] = useState<'diff' | 'sql'>('diff');
  const [rewriteSource, setRewriteSource] = useState('');

  const changeSql = (value: string) => {
    if (busy.current) return;
    setSql(value);
    setRaw(undefined);
    setRewritten(undefined);
    setResult(undefined);
  };
  const run = async (next: 'raw' | 'preview' | 'execute') => {
    const currentSql = readSql();
    if (busy.current || !currentSql.trim()) return;
    busy.current = true;
    setAction(next);
    if (next === 'raw') setRaw(undefined);
    else { setRewritten(undefined); setResult(undefined); setRewriteSource(currentSql); }
    try {
      if (next === 'raw') setRaw({ data: await api.rawQuery(currentSql, maxRows) });
      else if (next === 'preview') setRewritten({ data: await api.previewRewrite(currentSql, userId) });
      else {
        // The server regenerates and validates the statement and binds for this identity.
        const data = await api.rewrite(currentSql, userId, maxRows);
        setRewritten({ data });
        setResult({ data });
      }
    } catch (e) {
      if (next === 'raw') setRaw(failure(e));
      else {
        const err = e as ApiError;
        const preview = err.body?.rewrite as RewritePreview | undefined;
        setRewritten(preview ? { data: preview } : failure(e));
        if (next === 'execute') setResult(failure(e));
      }
    } finally { busy.current = false; setAction(undefined); }
  };
  const disabled = Boolean(action);
  const selectedExample = examples.findIndex((example) => example.sql === sql);

  return <div className="sql-workbench-scroll">
      <div className="sql-workbench-grid">
        <section className="sql-pane" aria-label="原始区域">
          <Card size="small" className="sql-source-card" title={<div className="sql-source-title"><span>原始 SQL</span>
            <Select size="small" aria-label="SQL 示例" placeholder="选择 SQL 示例" className="sql-example-picker" disabled={disabled} value={selectedExample < 0 ? undefined : selectedExample} options={examples.map((example, index) => ({ value: index, label: example.label }))} onChange={(index) => changeSql(examples[index].sql)} />
          </div>} extra={<Space size={6}>
            <Button size="small" icon={<FormatPainterOutlined />} disabled={disabled || !sql.trim()} onClick={() => changeSql(formatSQL(readSql()))}>一键美化</Button>
            <Button size="small" type="primary" icon={<PlayCircleOutlined />} disabled={disabled || !sql.trim()} loading={action === 'raw'} onClick={() => void run('raw')}>原始 SQL 执行</Button>
          </Space>}>
            <div className="sql-context sql-original-context"><Tag>超级管理员 · 全量数据</Tag><Text type="secondary">⌘ / Ctrl + Enter 执行原始 SQL</Text>
              <Select size="small" aria-label="最大返回行数" className="sql-row-limit" disabled={disabled} value={maxRows} onChange={(value) => { setMaxRows(value); setRaw(undefined); setResult(undefined); }} options={[50, 200, 1000].map((n) => ({ value: n, label: `最多 ${n} 行` }))} />
            </div>
            <Input.TextArea ref={editor} aria-label="原始 SQL" className="mono sql-editor" value={sql} disabled={disabled} onChange={(e) => changeSql(e.target.value)} style={{ height: 320, resize: 'vertical' }} onKeyDown={(e) => {
              if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') { e.preventDefault(); void run('raw'); }
            }} />
          </Card>
          <Card size="small" title="原始结果" extra={<ResultSummary outcome={raw} />}><QueryOutput outcome={raw} loading={action === 'raw'} /></Card>
        </section>
        <section className="sql-pane" aria-label="改写区域">
          <Card size="small" className="sql-source-card" title="改写后的 SQL" extra={<Space size={6}>
            <Button size="small" icon={<ThunderboltOutlined />} disabled={disabled || !sql.trim()} loading={action === 'preview'} onClick={() => void run('preview')}>一键改写</Button>
            <Button size="small" type="primary" icon={<PlayCircleOutlined />} disabled={disabled || !sql.trim()} loading={action === 'execute'} onClick={() => void run('execute')}>改写后 SQL 执行</Button>
          </Space>}>
            <div className="sql-context">
              <Text type="secondary">权限身份</Text>
              <div className="sql-user-picker">
                <UserPicker size="small" disabled={disabled} value={userId} onChange={(id) => { if (busy.current) return; setUserId(id); setRewritten(undefined); setResult(undefined); }} style={{ minWidth: 0, width: '100%' }} />
              </div>
              <Segmented size="small" value={view} onChange={setView} options={[{ value: 'diff', label: 'Git diff' }, { value: 'sql', label: '完整 SQL' }]} />
            </div>
            <div className="sql-preview">
              {rewritten?.error ? <Alert type={rewritten.denied ? 'warning' : 'error'} showIcon message={rewritten.denied ? '改写拒绝' : '改写失败'} description={rewritten.error} /> : rewritten?.data ? <>
                {view === 'diff' ? <SqlDiff original={rewriteSource} rewritten={rewritten.data.rewritten_sql} /> : <SqlBlock sql={rewritten.data.rewritten_sql} maxHeight={320} format={false} />}
              </> : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={action === 'preview' || action === 'execute' ? '正在按员工权限改写…' : '点击“一键改写”查看最终 SQL 与差异'} />}
            </div>
            {rewritten?.data && <Space orientation="vertical" size="small" style={{ width: '100%', marginTop: 12 }}>
              <Space wrap><Text type="secondary">权限策略</Text>{rewritten.data.tables.map((t, i) => <Tag key={i}><span className="mono">{t.table}{t.alias !== t.table ? ` AS ${t.alias}` : ''}</span> <SchemeTag scheme={t.scheme} /></Tag>)}</Space>
              <Space wrap><Text type="secondary">绑定变量</Text>{Object.entries(rewritten.data.binds).map(([key, value]) => <Tag key={key} className="mono">:{key} = {String(value)}</Tag>)}{Object.keys(rewritten.data.binds).length === 0 && <Text type="secondary">无</Text>}</Space>
            </Space>}
          </Card>
          <Card size="small" title={`改写后结果 · 员工 #${userId}`} extra={<ResultSummary outcome={result} />}>
            {!result && rewritten?.data ? <Alert type="info" showIcon message="SQL 已改写，尚未执行" description="点击“改写后 SQL 执行”查看该员工权限范围内的结果。" /> : <QueryOutput outcome={result} loading={action === 'execute'} />}
          </Card>
        </section>
      </div>
    </div>;
}

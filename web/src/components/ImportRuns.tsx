import { useEffect, useState } from 'react';
import { Alert, Button, Collapse, Descriptions, Drawer, Space, Spin, Table, Tag, Typography } from 'antd';
import { sourceCall, type ImportRun } from '../dataSources';
import { CodeEditor } from './CodeEditor';
import { useI18n, type Translate } from '../ui/preferences';

const labels: Record<string, string> = { running: '运行中', succeeded: '成功', failed: '失败', stopped: '已停止', interrupted: '已中断', rolled_back: '已回滚', unknown: '结果未知' };
const colors: Record<string, string> = { running: 'processing', succeeded: 'success', failed: 'error', rolled_back: 'blue', unknown: 'warning', interrupted: 'warning' };
export const formatRunTime = (value?: string) => value ? new Date(value).toLocaleString() : '—';
export const importRunStatus = (run: ImportRun | undefined, t: Translate) => run
 ? <Tag color={colors[run.status]}>{t(labels[run.status] || run.status)}</Tag>
 : <Typography.Text type="secondary">{t('未运行')}</Typography.Text>;

export function ImportRunTable({ runs, onInspect, loading = false }: { runs: ImportRun[]; onInspect: (id: string) => void; loading?: boolean }) {
 const t = useI18n();
 return <Table<ImportRun> rowKey="id" loading={loading} dataSource={runs} size="small" scroll={{ x: 880 }} pagination={{ pageSize: 10 }} locale={{ emptyText: t('暂无运行记录') }} columns={[
  { title: t('任务名称'), dataIndex: 'task_name' }, { title: t('开始时间'), render: (_, run) => formatRunTime(run.started) },
  { title: t('触发方式'), render: (_, run) => t(run.trigger === 'trial' ? '回滚试运行' : run.trigger === 'schedule' ? '定时运行' : '手动运行') },
  { title: t('状态'), render: (_, run) => importRunStatus(run, t) }, { title: t('读取 / 影响行数'), render: (_, run) => `${run.read} / ${run.written}` },
  { title: t('操作'), render: (_, run) => <Button size="small" onClick={() => onInspect(run.id)}>{t('详情')}</Button> },
 ]} />;
}

export function ImportRunDetails({ databaseID, runID, onClose }: { databaseID: string; runID?: string; onClose: () => void }) {
 const t = useI18n();
 const [run, setRun] = useState<ImportRun>();
 const [error, setError] = useState('');
 const [stopping, setStopping] = useState(false);
 useEffect(() => {
  setRun(undefined); setError(''); setStopping(false);
  if (!runID) return;
  let active = true;
  let timer: number | undefined;
  const load = async () => {
   try {
    const next = await sourceCall<ImportRun>(databaseID, 'get_import_run', { id: runID });
    if (!active) return;
    setRun(next); setError('');
    if (next.status === 'running') timer = window.setTimeout(() => void load(), 1500);
   } catch (e) { if (active) setError((e as Error).message); }
  };
  void load();
  return () => { active = false; window.clearTimeout(timer); };
 }, [databaseID, runID]);
 const stop = async () => {
  setStopping(true); setError('');
  try { await sourceCall(databaseID, 'stop_import_run', { id: runID }); }
  catch (e) { setError((e as Error).message); }
  finally { setStopping(false); }
 };
 return <Drawer title={t('运行详情')} open={!!runID} onClose={onClose} size="large" extra={run?.status === 'running' && <Button danger loading={stopping} onClick={() => void stop()}>{t('停止运行')}</Button>}>
  {error && <Alert type="error" showIcon title={error} style={{ marginBottom: 16 }} />}
  {!run && !error && <Spin />}
  {run && <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
   <Descriptions column={1} items={[
    { key: 'task', label: t('任务名称'), children: run.task_name }, { key: 'id', label: t('运行编号'), children: run.id },
    { key: 'state', label: t('状态'), children: importRunStatus(run, t) }, { key: 'version', label: t('脚本版本'), children: run.revision },
    { key: 'started', label: t('开始时间'), children: formatRunTime(run.started) }, { key: 'finished', label: t('结束时间'), children: formatRunTime(run.finished) },
    { key: 'counts', label: t('读取 / 影响行数'), children: `${run.read} / ${run.written}` },
   ]} />
   {run.error && <Alert type="error" showIcon title={run.error} />}
   <Typography.Text type="secondary">{t('影响行数为脚本执行计数；试运行、失败或停止时不代表已提交。')}</Typography.Text>
   <Typography.Text strong>{t('运行日志')}</Typography.Text><pre className="source-run-log">{run.logs?.join('\n') || t('暂无日志')}</pre>
   <Collapse items={[
    { key: 'parameters', label: t('本次运行参数'), children: <pre>{JSON.stringify(run.parameters || {}, null, 2)}</pre> },
    { key: 'checkpoint', label: t('运行进度'), children: <pre>{JSON.stringify(run.checkpoint || {}, null, 2)}</pre> },
    { key: 'script', label: t('本次执行脚本'), children: <CodeEditor readOnly value={run.script || ''} height="360px" /> },
   ]} />
  </Space>}
 </Drawer>;
}

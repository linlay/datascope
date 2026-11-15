import { useEffect, useState } from 'react';
import { ReloadOutlined } from '@ant-design/icons';
import { Alert, Button, Card, Select, Space } from 'antd';
import type { Database } from '../api';
import { sourceCall, type SourceOverview } from '../dataSources';
import { ImportRunDetails, ImportRunTable } from '../components/ImportRuns';
import { navigate, selectResource, useRoute } from '../navigation';
import { useI18n } from '../ui/preferences';

export default function DataRunHistory({ db }: { db: Database }) {
 const t = useI18n();
 const route = useRoute();
 const task = route.params.get('task') || undefined;
 const [data, setData] = useState<SourceOverview>({ connections: [], tasks: [], runs: [] });
 const [loading, setLoading] = useState(true);
 const [error, setError] = useState('');
 const [refreshKey, setRefreshKey] = useState(0);
 useEffect(() => {
  let active = true, pending = false;
  setLoading(true);
  const load = async () => {
   if (pending) return;
   pending = true;
   try { const next = await sourceCall<SourceOverview>(db.id, 'list_data_sources'); if (active) { setData(next); setError(''); } }
   catch (e) { if (active) setError((e as Error).message); }
   finally { pending = false; if (active) setLoading(false); }
  };
  void load();
  const timer = window.setInterval(() => void load(), 3000);
  return () => { active = false; window.clearInterval(timer); };
 }, [db.id, refreshKey]);
 const names = new Map(data.runs.map(run => [run.task_id, run.task_name]));
 data.tasks.forEach(entry => names.set(entry.id, entry.name));
 if (task && !names.has(task)) names.set(task, task);
 return <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
  {error && <Alert type="error" showIcon title={t('操作失败')} description={error} />}
  <Card>
   <div className="source-list-toolbar">
    <Select aria-label={t('筛选导入任务')} placeholder={t('全部任务')} value={task} allowClear showSearch optionFilterProp="label" style={{ width: 300, maxWidth: '100%' }} options={Array.from(names, ([value, label]) => ({ value, label }))} onChange={value => navigate('data-runs', undefined, { task: value })} />
    <Button aria-label={t('刷新')} icon={<ReloadOutlined />} loading={loading} onClick={() => setRefreshKey(value => value + 1)} />
   </div>
   <ImportRunTable runs={data.runs.filter(run => !task || run.task_id === task)} loading={loading} onInspect={id => selectResource(id)} />
  </Card>
  <ImportRunDetails databaseID={db.id} runID={route.selection} onClose={() => selectResource(undefined)} />
 </Space>;
}

import { useEffect, useState } from 'react';
import { Alert, Card, Select, Space, Typography, message } from 'antd';
import { managementCall, type Database } from '../api';
import { useAuth } from '../Auth';
import { useNavigationBlocker } from '../navigation';
import { useI18n } from '../ui/preferences';
import { SaveButton } from './IconButton';

export function QueryDisplaySettings({ db, onSaved }: { db: Database; onSaved: () => void }) {
 const t = useI18n();
 const { user } = useAuth();
 const [current, setCurrent] = useState(db);
 const [viewID, setViewID] = useState(db.query_view_id || '');
 const [busy, setBusy] = useState(false), [error, setError] = useState('');
 const canWrite = user.system_admin || !!db.permissions?.includes('view.write');
 const dirty = viewID !== (current.query_view_id || '');
 useNavigationBlocker(dirty, t('放弃查询展示设置的未保存修改？'), busy);
 useEffect(() => {
  setCurrent(db); setViewID(db.query_view_id || ''); setError('');
 }, [db.id, db.revision]);
 const save = async () => {
  if (busy || !dirty || !canWrite) return;
  setBusy(true); setError('');
  try {
   const saved = await managementCall<Database>('configure_views', {
    database_id: current.id,
    database: { ...current, discovery: undefined, query_view_id: viewID },
   });
   setCurrent(saved); setViewID(saved.query_view_id || '');
   onSaved(); message.success(t('查询展示设置已保存'));
  } catch (e) { setError((e as Error).message); }
  finally { setBusy(false); }
 };
 return <Card title={t('查询展示设置')} extra={<SaveButton disabled={!canWrite || !dirty} loading={busy} onClick={() => void save()} />}>
  <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
   {error && <Alert type="error" showIcon title={error} />}
   <div>
    <Typography.Text>{t('默认查询视图')}</Typography.Text>
    <Select aria-label={t('默认查询视图')} style={{ marginTop: 8, width: '100%', maxWidth: 480, display: 'flex' }}
     value={viewID} disabled={busy || !canWrite} onChange={setViewID}
     options={[{ value: '', label: t('使用原始查询结果') }, ...(current.views || []).filter(v => v.published).map(v => ({ value: v.id, label: v.name }))]} />
   </div>
   <Typography.Text type="secondary">{t('作用于当前数据库的 MCP 查询：未指定视图时推荐此视图。选择原始查询结果可取消推荐；SQL 工作台始终显示结果表格。')}</Typography.Text>
  </Space>
 </Card>;
}

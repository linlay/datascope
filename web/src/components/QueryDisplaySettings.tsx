import { useEffect, useState } from 'react';
import { Alert, Button, Modal, Select, Space, Typography, message } from 'antd';
import { managementCall, type Database } from '../api';
import { useAuth } from '../Auth';
import { useNavigationBlocker } from '../navigation';
import { useI18n } from '../ui/preferences';
import { SaveButton } from './IconButton';

export function QueryDisplaySettings({ db, onSaved, disabled }: { db: Database; onSaved: () => void; disabled?: boolean }) {
 const t = useI18n();
 const { user } = useAuth();
 const [open, setOpen] = useState(false);
 const [current, setCurrent] = useState(db);
 const [viewID, setViewID] = useState(db.query_view_id || '');
 const [busy, setBusy] = useState(false), [error, setError] = useState('');
 const canWrite = user.system_admin || !!db.permissions?.includes('view.write');
 const dirty = viewID !== (current.query_view_id || '');
 useNavigationBlocker(open && dirty, t('放弃 MCP 展示推荐的未保存修改？'), busy);
 useEffect(() => {
  if (!open || !dirty) { setCurrent(db); setViewID(db.query_view_id || ''); setError(''); }
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
   setOpen(false); onSaved(); message.success(t('MCP 展示推荐已保存'));
  } catch (e) { setError((e as Error).message); }
  finally { setBusy(false); }
 };
 const show = () => { setCurrent(db); setViewID(db.query_view_id || ''); setError(''); setOpen(true); };
 const close = () => {
  if (busy) return;
  if (dirty) Modal.confirm({ title: t('放弃 MCP 展示推荐的未保存修改？'), onOk: () => setOpen(false) });
  else setOpen(false);
 };
 return <>
  <Button size="small" disabled={disabled} onClick={show}>{t('MCP 展示推荐')}</Button>
  <Modal title={t('MCP 展示推荐')} open={open} onCancel={close} destroyOnHidden maskClosable={false} closable={!busy} footer={<Space><Button disabled={busy} onClick={close}>{t('关闭')}</Button><SaveButton disabled={!canWrite || !dirty} loading={busy} onClick={() => void save()} /></Space>}>
  <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
   {error && <Alert type="error" showIcon title={error} />}
   <div className="configuration-inline-field">
    <Typography.Text>{t('默认查询视图')}</Typography.Text>
    <Select aria-label={t('默认查询视图')} style={{ width: '100%', maxWidth: 480, display: 'flex' }}
     value={viewID} disabled={busy || !canWrite} onChange={setViewID}
     options={[{ value: '', label: t('使用原始查询结果') }, ...(current.views || []).filter(v => v.published).map(v => ({ value: v.id, label: v.name }))]} />
   </div>
   <Typography.Text type="secondary">{t('可选：向支持 HTML 视图的 MCP 客户端推荐查询展示方式。客户端决定是否展示；此设置不改变查询数据，也不影响 SQL 工作台。无需推荐时选择使用原始查询结果。')}</Typography.Text>
  </Space>
  </Modal>
 </>;
}

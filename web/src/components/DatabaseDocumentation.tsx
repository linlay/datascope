import { lazy, Suspense, useEffect, useState } from 'react';
import { Alert, Button, Divider, Empty, Input, Modal, Space, Spin, Tabs, Typography, message } from 'antd';
import { managementCall, type Database } from '../api';
import { useAuth } from '../Auth';
import { useNavigationBlocker } from '../navigation';
import { useI18n } from '../ui/preferences';
import { SaveButton } from './IconButton';
import { InfoCircleOutlined } from '@ant-design/icons';

const Markdown = lazy(() => import('./KnowledgeContent'));

export function DatabaseDocumentationSettings({ db, onSaved }: { db: Database; onSaved: () => void }) {
 const t = useI18n();
 const { user } = useAuth();
 const [open, setOpen] = useState(false);
 const [mode, setMode] = useState('preview');
 const [current, setCurrent] = useState(db);
 const [description, setDescription] = useState(db.description || '');
 const [documentation, setDocumentation] = useState(db.documentation || '');
 const [busy, setBusy] = useState(false), [error, setError] = useState('');
 const canWrite = user.system_admin || !!db.permissions?.includes('config.write');
 const dirty = description !== (current.description || '') || documentation !== (current.documentation || '');
 useNavigationBlocker(open && dirty, t('放弃数据库说明的未保存修改？'), busy);
 useEffect(() => {
  // Keep a draft when another database setting is saved. The original revision
  // makes the next save report a conflict instead of overwriting unseen changes.
  if (!open || !dirty) { setCurrent(db); setDescription(db.description || ''); setDocumentation(db.documentation || ''); }
 }, [db.id, db.revision]);
 const save = async () => {
  if (busy || !dirty || !canWrite) return;
  setBusy(true); setError('');
  try {
   const saved = await managementCall<Database>('update_database', { database_id: db.id, database: {
    id: db.id, revision: current.revision, description, documentation,
   } });
   setCurrent(saved); setDescription(saved.description || ''); setDocumentation(saved.documentation || '');
   setOpen(false); onSaved(); message.success(t('数据库说明已保存'));
  } catch (e) { setError((e as Error).message); }
  finally { setBusy(false); }
 };
 const show = () => { setCurrent(db); setDescription(db.description || ''); setDocumentation(db.documentation || ''); setError(''); setMode('edit'); setOpen(true); };
 const close = () => {
  if (busy) return;
  if (dirty) Modal.confirm({ title: t('放弃数据库说明的未保存修改？'), onOk: () => setOpen(false) });
  else setOpen(false);
 };
 return <>
  <Button disabled={!canWrite} onClick={show}>{t('编辑说明')}</Button>
  <Modal title={`${db.name} · ${t('使用说明')}`} open={open} onCancel={close} width={900} destroyOnHidden maskClosable={false} closable={!busy} styles={{ body: { maxHeight: '70vh', overflowY: 'auto' } }} footer={<Space><Button disabled={busy} onClick={close}>{t('关闭')}</Button>{canWrite && (mode === 'edit' || dirty) && <SaveButton disabled={!dirty} loading={busy} onClick={() => void save()} />}</Space>}>
   {error && <Alert type="error" showIcon title={error} style={{ marginBottom: 12 }} />}
   <Tabs activeKey={mode} onChange={setMode} items={[
    { key: 'preview', label: t('预览'), children: <>
     {description && <Typography.Paragraph style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{description}</Typography.Paragraph>}
     {description && documentation && <Divider />}
     {documentation && <Suspense fallback={<Spin />}><Markdown content={documentation} copyCode /></Suspense>}
     {!description && !documentation && <Empty description={t('暂无数据库说明')} />}
    </> },
    { key: 'edit', label: t('编辑'), disabled: !canWrite || busy, children: <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
     <div><Typography.Text>{t('描述')}</Typography.Text>
      <Input.TextArea aria-label={t('描述')} value={description} disabled={busy || !canWrite} onChange={e => setDescription(e.target.value)} autoSize={{ minRows: 2, maxRows: 5 }} placeholder={t('简要说明这个数据库包含什么、适合查询什么。')} />
     </div>
     <div><Typography.Text>{t('使用说明（Markdown）')}</Typography.Text>
      <Input.TextArea aria-label={t('使用说明（Markdown）')} value={documentation} disabled={busy || !canWrite} onChange={e => setDocumentation(e.target.value)} autoSize={{ minRows: 12, maxRows: 30 }} placeholder={t('填写数据范围、主要表关系、统计口径和 SQL 查询示例。')} />
     </div>
     <Typography.Text type="secondary">{t('简介和说明对当前库的使用者及 MCP 客户端可见，请填写可共享的业务资料。说明不会改变访问权限。')}</Typography.Text>
    </Space> },
   ]} />
  </Modal>
 </>;
}

export function DatabaseDocumentation({ db, label }: { db: Database; label?: string }) {
 const t = useI18n();
 const [open, setOpen] = useState(false);
 return <>
  <Button type="text" size="small" icon={<InfoCircleOutlined />} aria-label={t('使用说明')} onClick={() => setOpen(true)}>{label || t('使用说明')}</Button>
  <Modal title={`${db.name} · ${t('使用说明')}`} open={open} onCancel={() => setOpen(false)} footer={<Button onClick={() => setOpen(false)}>{t('关闭')}</Button>} width={900} styles={{ body: { maxHeight: '70vh', overflowY: 'auto' } }}>
   {db.description && <Typography.Paragraph style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{db.description}</Typography.Paragraph>}
   {db.description && db.documentation && <Divider />}
   {db.documentation && <Suspense fallback={<Spin />}><Markdown content={db.documentation} copyCode /></Suspense>}
   {!db.description && !db.documentation && <Empty description={t('暂无数据库说明')} />}
  </Modal>
 </>;
}

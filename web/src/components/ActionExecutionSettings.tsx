import { useEffect, useState } from 'react';
import { Alert, Button, Form, Input, Modal, Space, Switch, Tooltip, message } from 'antd';
import { managementCall, type Database, type DatabaseConnection } from '../api';
import { useI18n } from '../ui/preferences';

export function ActionExecutionSettings({ db, dirty, onSaved, onError }: { db: Database; dirty: boolean; onSaved: () => void; onError: (error: string) => void }) {
 const t = useI18n();
 const [busy, setBusy] = useState(false);
 const [open, setOpen] = useState(false);
 const [error, setError] = useState('');
 const [form] = Form.useForm();
 const canWrite = !!db.permissions?.includes('action.write');
 useEffect(() => { setOpen(false); setError(''); }, [db.id]);
 const save = async (enabled: boolean, connection = db.write_connection, password = '') => {
  setBusy(true); setError(''); onError('');
  try {
   await managementCall('configure_actions', { database_id: db.id, database: { ...db, write_enabled: enabled, write_connection: connection }, write_password: password });
   setOpen(false); onSaved(); message.success(t('动作执行设置已保存'));
  } catch (e) { const text = (e as Error).message; setError(text); if (!open) onError(text); } finally { setBusy(false); }
 };
 const configure = () => { form.setFieldsValue({ username: db.write_connection?.username || '', password: '' }); setError(''); setOpen(true); };
 const toggle = (enabled: boolean) => {
  if (enabled && db.driver !== 'sqlite' && !db.write_connection) { configure(); return; }
  void save(enabled);
 };
 const saveConnection = async () => {
  try {
   const values = await form.validateFields();
   const connection: DatabaseConnection = { ...db.connection!, username: values.username, password_env: values.password ? undefined : db.write_connection?.password_env, credential_id: values.password ? undefined : db.write_connection?.credential_id };
   await save(true, connection, values.password);
  } catch { /* Form displays validation errors beside each field. */ }
 };
 return <>
  <Space wrap size="small">
   <Tooltip title={dirty ? t('请先保存动作修改') : t('关闭后停止执行本库动作，查询不受影响。')}>
    <Space size={6}><Switch size="small" aria-label={t('允许执行动作')} checked={!!db.write_enabled} disabled={!canWrite || dirty} loading={busy} onChange={toggle} /><span>{t('允许执行动作')}</span></Space>
   </Tooltip>
   {db.driver !== 'sqlite' && <Button size="small" disabled={!canWrite || dirty || busy} onClick={configure}>{t('写入账号')}</Button>}
  </Space>
  <Modal title={t('写入账号')} open={open} onCancel={() => setOpen(false)} onOk={() => void saveConnection()} confirmLoading={busy} okText={t('保存并启用')}>
   {error && <Alert type="error" title={error} style={{ marginBottom: 16 }} />}
   <Form form={form} layout="vertical">
    <Form.Item name="username" label={t('写入账号')} rules={[{ required: true }]}><Input autoComplete="off" /></Form.Item>
    <Form.Item name="password" label={t('连接密码')} rules={[{ required: !db.write_connection }]}><Input.Password autoComplete="new-password" placeholder={t('留空保留原密码')} /></Form.Item>
   </Form>
  </Modal>
 </>;
}

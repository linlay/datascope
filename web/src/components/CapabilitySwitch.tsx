import { useRef, useState } from 'react';
import { Button, Form, Input, Modal, Space, Switch, Tooltip, message } from 'antd';
import { managementCall, type Database, type DatabaseConnection } from '../api';
import { useAuth } from '../Auth';
import { useI18n } from '../ui/preferences';

export type Capability = 'actions' | 'knowledge' | 'views';
export const actionTools = ['list_actions', 'describe_action', 'invoke_action'];
export const knowledgeTools = ['search_knowledge', 'read_knowledge'];
export function capabilityEnabled(db: Database, capability: Capability): boolean {
 if (db.capabilities) return db.capabilities[capability];
 if (capability === 'actions') return !!db.write_enabled;
 if (capability === 'knowledge') return db.knowledge_enabled ?? (db.tools || []).some(name => knowledgeTools.includes(name));
 return db.views_enabled ?? !!db.views?.some(view => view.published);
}
export function CapabilitySwitch({ db, capability, disabled, onSaved, onBusyChange }: {
 db: Database; capability: Capability; disabled?: boolean; onSaved: (db: Database) => void; onBusyChange?: (busy: boolean) => void;
}) {
 const t = useI18n(), { user } = useAuth();
 const [busy, setBusy] = useState(false), [open, setOpen] = useState(false);
 const [form] = Form.useForm();
 const snapshot = useRef(db);
 const desiredEnabled = useRef(true);
 const latestID = useRef(db.id); latestID.current = db.id;
 const permissions = db.permissions || [];
 const domain = { actions: 'action.write', knowledge: 'knowledge.write', views: 'view.write' }[capability];
 const canChange = user.system_admin || permissions.includes(domain) || capability !== 'actions' && permissions.includes('mcp.write');
 const label = t({ actions: '动作总开关', knowledge: '知识总开关', views: '视图总开关' }[capability]);
 const save = async (enabled: boolean, source = db, connection?: DatabaseConnection, password = '') => {
  if (busy) return;
  setBusy(true); onBusyChange?.(true);
  try {
   const saved = await managementCall<Database>('configure_capability', {
    database_id: source.id, revision: source.revision, capability, enabled,
    ...(connection ? { database: { id: source.id, revision: source.revision, write_connection: connection }, write_password: password } : {}),
   });
   if (latestID.current !== saved.id) return;
   setOpen(false); onSaved(saved); message.success(t('总开关已保存'));
  } catch (e) { if (latestID.current === source.id) message.error((e as Error).message); }
  finally { setBusy(false); onBusyChange?.(false); }
 };
 const toggle = (enabled: boolean) => {
  if (capability === 'actions' && enabled && db.driver !== 'sqlite' && !db.write_connection) {
   snapshot.current = db; desiredEnabled.current = true; form.setFieldsValue({ username: '', password: '' }); setOpen(true); return;
  }
  void save(enabled);
 };
 const saveConnection = async () => {
  const values = await form.validateFields().catch(() => undefined);
  if (!values) return;
  const source = snapshot.current;
  const connection: DatabaseConnection = { ...source.connection!, username: values.username, credential_id: values.password ? undefined : source.write_connection?.credential_id, password_env: values.password ? undefined : source.write_connection?.password_env };
  await save(desiredEnabled.current, source, connection, values.password);
 };
 return <>
  <Tooltip title={disabled ? t('请先保存当前修改') : label}>
   <span><Switch size="small" aria-label={label} checked={capabilityEnabled(db, capability)} checkedChildren={t('已打开')} unCheckedChildren={t('打开')} disabled={!canChange || disabled || busy} loading={busy} onChange={toggle} /></span>
  </Tooltip>
  {capability === 'actions' && db.driver !== 'sqlite' && <Button size="small" disabled={!canChange || disabled || busy} onClick={() => { snapshot.current = db; desiredEnabled.current = capabilityEnabled(db, capability); form.setFieldsValue({ username: db.write_connection?.username || '', password: '' }); setOpen(true); }}>{t('写入账号')}</Button>}
  <Modal title={t('写入账号')} open={open} onCancel={() => { if (!busy) setOpen(false); }} maskClosable={false} footer={<Space><Button disabled={busy} onClick={() => setOpen(false)}>{t('取消')}</Button><Button type="primary" loading={busy} onClick={() => void saveConnection()}>{t('保存')}</Button></Space>}>
   <Form className="configuration-form" form={form} layout="horizontal" labelAlign="left" labelWrap>
    <Form.Item name="username" label={<span className="configuration-label-text">{t('写入账号')}</span>} rules={[{ required: true, whitespace: true }]}><Input autoComplete="off" /></Form.Item>
    <Form.Item name="password" label={<span className="configuration-label-text">{t('连接密码')}</span>} rules={[{ required: !snapshot.current.write_connection }]}><Input.Password autoComplete="new-password" /></Form.Item>
   </Form>
  </Modal>
 </>;
}

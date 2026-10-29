import { useEffect, useState } from 'react';
import { Alert, Button, Card, Form, Input, InputNumber, Modal, Select, Space, Switch, Table, Typography, message } from 'antd';
import { managementCall, type Database, type ActionDefinition, type DatabaseConnection } from '../api';
import { useI18n } from '../ui/preferences';

const example: ActionDefinition = {
 name: 'update_record', title: '更新记录', description: '更新当前用户可见的记录', confirm: true,
 parameters: { id: { type: 'integer', description: '记录编号' }, value: { type: 'string', description: '新值', max_length: 200 } },
 script: 'function run(args, ctx) {\n  const row = my.one`SELECT id FROM records WHERE id = ${args.id}`;\n  if (!row) deny("记录不存在或无权查看");\n  const n = db.exec`UPDATE records SET value = ${args.value} WHERE id = ${args.id}`;\n  if (n !== 1) deny("记录已变化，请重试");\n  return { id: args.id, updated: n };\n}',
};
export function Actions({ db, onSaved }: { db: Database; onSaved: () => void }) {
 const t = useI18n();
 const canWrite = db.permissions?.includes('action.write');
 const [items, setItems] = useState<ActionDefinition[]>(db.actions || []);
 const [enabled, setEnabled] = useState(!!db.write_enabled);
 const [username, setUsername] = useState(db.write_connection?.username || '');
 const [passwordEnv, setPasswordEnv] = useState(db.write_connection?.password_env || '');
 const [password, setPassword] = useState('');
 const [editing, setEditing] = useState<number>();
 const [busy, setBusy] = useState(false);
 const [error, setError] = useState('');
 const [parameters, setParameters] = useState('{}');
 const [trialName, setTrialName] = useState<string>();
 const [identity, setIdentity] = useState<number>(db.business_user_id || 1);
 const [args, setArgs] = useState('{}');
 const [result, setResult] = useState<unknown>();
 const [form] = Form.useForm();
 useEffect(() => { setItems(db.actions || []); setEnabled(!!db.write_enabled); setUsername(db.write_connection?.username || ''); setPasswordEnv(db.write_connection?.password_env || ''); setPassword(''); setResult(undefined); setTrialName(undefined); setEditing(undefined); setError(''); }, [db.id, db.revision]);
 const edit = (i: number) => { const a = items[i] || { ...example, name: '', title: '', description: '' }; form.setFieldsValue(a); setParameters(JSON.stringify(a.parameters, null, 2)); setEditing(i); setError(''); };
 const apply = async () => { try { const v = await form.validateFields(); const p = JSON.parse(parameters); if (!p || Array.isArray(p) || typeof p !== 'object') throw new Error(t('参数定义必须是 JSON 对象')); const next = [...items]; next[editing!] = { ...v, parameters: p }; setItems(next); setEditing(undefined); } catch (e) { setError((e as Error).message); } };
 const save = async () => {
  setBusy(true); setError('');
  try {
   let write_connection: DatabaseConnection | undefined;
   if (db.driver !== 'sqlite' && username) { write_connection = { ...db.connection!, username, password_env: password ? undefined : passwordEnv || undefined, credential_id: password || passwordEnv ? undefined : db.write_connection?.credential_id }; }
   await managementCall('configure_actions', { database_id: db.id, database: { ...db, actions: items, write_enabled: enabled, write_connection, discovery: undefined }, write_password: password });
   setPassword(''); onSaved(); message.success(t('业务操作配置已保存'));
  } catch (e) { setError((e as Error).message); } finally { setBusy(false); }
 };
 const trial = async () => {
  setBusy(true); setError(''); setResult(undefined);
  try { const arguments_ = JSON.parse(args); if (!arguments_ || Array.isArray(arguments_) || typeof arguments_ !== 'object') throw new Error(t('调用参数必须是 JSON 对象')); setResult(await managementCall('try_action', { database_id: db.id, name: trialName, business_user_id: identity, arguments: arguments_, request_id: crypto.randomUUID() })); }
  catch (e) { setError((e as Error).message); } finally { setBusy(false); }
 };
 const dirty = JSON.stringify(items) !== JSON.stringify(db.actions || []) || enabled !== !!db.write_enabled || username !== (db.write_connection?.username || '') || passwordEnv !== (db.write_connection?.password_env || '') || !!password;
 return <Card title={t('业务操作 Action')} extra={<Button type="primary" loading={busy} disabled={!canWrite || !dirty} onClick={() => void save()}>{t('保存业务操作')}</Button>}>
  <Space orientation="vertical" style={{ width: '100%' }} size="middle">
   {error && <Alert type="error" showIcon title={error} />}
   <Space><Switch aria-label={t('启用独立写入连接')} checked={enabled} disabled={!canWrite} onChange={setEnabled} /><Typography.Text>{t('启用独立写入连接')}</Typography.Text></Space>
   {db.driver !== 'sqlite' && <Space wrap><Input aria-label={t('写入账号')} placeholder={t('写入账号')} value={username} disabled={!canWrite} onChange={e => setUsername(e.target.value)} /><Input.Password aria-label={t('写入密码')} placeholder={t('写入密码（留空保留）')} value={password} disabled={!canWrite} onChange={e => setPassword(e.target.value)} autoComplete="new-password" /><Input aria-label="password_env" placeholder="DATASCOPE_DB_WRITE_PASSWORD" value={passwordEnv} disabled={!canWrite} onChange={e => setPasswordEnv(e.target.value)} /></Space>}
   <Typography.Paragraph type="secondary">{t('每次操作在独立事务中执行。my.one / my.all 沿用查询权限；db.one / db.all 读取可信数据，db.exec 写入。SQL 只接受模板写法。确认操作先预览并回滚，再由业务账号本人打开确认页登录执行。')}</Typography.Paragraph>
   <Button disabled={!canWrite} onClick={() => edit(items.length)}>{t('新增业务操作')}</Button>
   <Table rowKey="name" pagination={false} dataSource={items} columns={[
    { title: t('名称'), dataIndex: 'name' }, { title: t('标题'), dataIndex: 'title' }, { title: t('说明'), dataIndex: 'description' }, { title: t('人工确认'), render: (_, a: ActionDefinition) => a.confirm ? t('是') : t('否') },
    { title: t('操作'), render: (_, a: ActionDefinition, i: number) => <Space><Button disabled={!canWrite} onClick={() => edit(i)}>{t('编辑')}</Button><Button danger disabled={!canWrite} onClick={() => Modal.confirm({ title: t('删除业务操作？'), onOk: () => setItems(items.filter((_, j) => j !== i)) })}>{t('删除')}</Button><Button disabled={dirty || !enabled || !db.permissions?.includes('impersonate') || !canWrite} onClick={() => { setTrialName(a.name); setResult(undefined); }}>{t('试运行')}</Button></Space> },
   ]} />
   <Modal className="action-dialog" styles={{ body: { maxHeight: '72vh', overflowY: 'auto' } }} title={t('编辑业务操作')} open={editing !== undefined} width={900} onCancel={() => setEditing(undefined)} onOk={() => void apply()}>
    {error && <Alert type="error" title={error} />}
    <Form form={form} layout="vertical"><Form.Item name="name" label={t('名称')} rules={[{ required: true, pattern: /^[a-zA-Z][a-zA-Z0-9_]{0,63}$/ }]}><Input /></Form.Item><Form.Item name="title" label={t('标题')} rules={[{ required: true }]}><Input /></Form.Item><Form.Item name="description" label={t('说明')} rules={[{ required: true }]}><Input /></Form.Item><Form.Item name="confirm" label={t('人工确认')} valuePropName="checked"><Switch /></Form.Item><Form.Item label={t('参数定义 JSON')}><Input.TextArea className="mono" rows={7} value={parameters} onChange={e => setParameters(e.target.value)} /></Form.Item><Form.Item name="script" label="JavaScript · run(args, ctx)" rules={[{ required: true }]}><Input.TextArea className="mono" rows={14} /></Form.Item></Form>
   </Modal>
   <Modal className="action-dialog" styles={{ body: { maxHeight: '72vh', overflowY: 'auto' } }} title={t('试运行（始终回滚）')} open={!!trialName} width={900} onCancel={() => setTrialName(undefined)} onOk={() => void trial()} confirmLoading={busy} okText={t('执行预览')}>
    {error && <Alert type="error" title={error} />}<Space orientation="vertical" style={{ width: '100%' }}><Select style={{ width: 320 }} value={trialName} onChange={setTrialName} options={db.actions?.map(a => ({ value: a.name, label: a.title }))} /><Typography.Text>{t('业务身份编号')}</Typography.Text><InputNumber min={1} precision={0} value={identity} onChange={v => setIdentity(v || 1)} /><Typography.Text>{t('调用参数 JSON')}</Typography.Text><Input.TextArea className="mono" rows={6} value={args} onChange={e => setArgs(e.target.value)} />{result !== undefined && <pre style={{ whiteSpace: 'pre-wrap', maxHeight: 400, overflow: 'auto' }}>{JSON.stringify(result, null, 2)}</pre>}</Space>
   </Modal>
  </Space>
 </Card>;
}

import { useEffect, useState } from 'react';
import { Alert, Button, Card, Checkbox, Form, Input, InputNumber, Modal, Select, Space, Switch, Table, Tag, Typography, message } from 'antd';
import { databases, managementCall, type Database } from '../api';

type Account = { id?: number; database_id: string; username: string; name: string; business_user_id: number; permissions: string[]; active: boolean; revision: number };
export function BusinessAccounts({ db, onSaved }: { db: Database; onSaved: () => void }) {
 const [accounts, setAccounts] = useState<Account[]>([]);
 const [editing, setEditing] = useState<Account>();
 const [error, setError] = useState<string>();
 const [busy, setBusy] = useState(false);
 const [form] = Form.useForm();
 const [authForm] = Form.useForm();
 const local = db.authentication?.type === 'local';
 const load = async () => {
  try { setAccounts(local ? await managementCall<Account[]>('list_business_accounts', { database_id: db.id }) : []); setError(undefined); }
  catch (e) { setError((e as Error).message); }
 };
 useEffect(() => { authForm.setFieldsValue(db.authentication || { type: 'platform', label: '', employee_directory: false }); void load(); }, [db.id, db.revision]);
 const saveAuth = async () => {
  try { const authentication = await authForm.validateFields(); setBusy(true); await databases.save({ ...db, authentication }); onSaved(); message.success('业务认证来源已保存，请重新授权连接器'); }
  catch (e) { setError((e as Error).message); } finally { setBusy(false); }
 };
 const saveAccount = async () => {
  try {
   const { password, ...values } = await form.validateFields(); setBusy(true);
   await managementCall('save_business_account', { database_id: db.id, account: { ...values, database_id: db.id }, password: password || '', revision: editing?.revision || 0 });
   setEditing(undefined); await load(); message.success('业务账号已保存，旧令牌已撤销');
  } catch (e) { setError((e as Error).message); } finally { setBusy(false); }
 };
 const edit = (account: Account) => { setEditing(account); form.resetFields(); form.setFieldsValue(account); };
 return <Space orientation="vertical" style={{ width: '100%' }}>
  <Card title={`业务认证来源 · ${db.name}`}>
   <Typography.Paragraph>每个数据库有独立的业务账号域。业务账号只用于该库的 MCP 登录，与平台管理账号、其他数据库的同名账号分别设置密码和权限。</Typography.Paragraph>
   {error && <Alert type="error" title={error} />}
   <Form form={authForm} layout="vertical">
    <Form.Item name="type" label="认证来源" rules={[{ required: true }]}><Select options={[{ value: 'local', label: '独立模拟业务账号（当前数据库）' }, { value: 'platform', label: '平台账号（旧配置兼容）' }]} /></Form.Item>
    <Form.Item name="label" label="员工登录页名称"><Input maxLength={40} /></Form.Item>
    <Form.Item name="employee_directory" label="同时校验本库 employees 员工目录" valuePropName="checked"><Switch /></Form.Item>
    <Typography.Paragraph type="secondary">证券 demo 开启员工目录校验，工号和业务身份必须匹配且员工在职。其他数据库可关闭校验，由模拟业务账号提供业务身份编号。切换认证来源后，连接器需重新授权。</Typography.Paragraph>
    <Button loading={busy} onClick={() => void saveAuth()}>保存认证来源</Button>
   </Form>
  </Card>
  {local && <Card title="当前数据库的模拟业务账号" extra={<Button onClick={() => edit({ database_id: db.id, username: '', name: '', business_user_id: 1, permissions: ['query', 'knowledge.read'], active: true, revision: 0 })}>创建业务账号</Button>}>
   <Table rowKey="username" dataSource={accounts} columns={[
    { title: '工号 / 账号', dataIndex: 'username' }, { title: '姓名', dataIndex: 'name' }, { title: '业务身份', dataIndex: 'business_user_id' },
    { title: '状态', render: (_, a: Account) => <Tag>{a.active ? '正常' : '已禁用'}</Tag> },
    { title: '操作', render: (_, a: Account) => <Button onClick={() => edit(a)}>编辑 / 重置密码</Button> },
   ]} />
  </Card>}
  <Modal title={editing?.revision ? '编辑业务账号' : '创建业务账号'} open={!!editing} onCancel={() => setEditing(undefined)} onOk={() => void saveAccount()} confirmLoading={busy}>
   <Form form={form} layout="vertical">
    <Form.Item name="username" label="工号 / 账号" rules={[{ required: true }]}><Input disabled={!!editing?.revision} /></Form.Item>
    <Form.Item name="name" label="姓名" rules={[{ required: true }]}><Input /></Form.Item>
    <Form.Item name="business_user_id" label="业务身份编号（用于 SQL 行权限）" rules={[{ required: true }]}><InputNumber min={1} precision={0} /></Form.Item>
    <Form.Item name="password" label={editing?.revision ? '新密码（留空保留原密码）' : '初始密码'} rules={[{ required: !editing?.revision, min: 12 }]}><Input.Password autoComplete="new-password" /></Form.Item>
    <Form.Item name="permissions" label="业务能力"><Checkbox.Group options={[{ value: 'query', label: '权限查询' }, { value: 'knowledge.read', label: '读取知识' }, { value: 'raw_query', label: '全量只读查询（绕过行权限）' }]} /></Form.Item>
    <Form.Item name="active" label="启用账号" valuePropName="checked"><Switch /></Form.Item>
   </Form>
  </Modal>
 </Space>;
}

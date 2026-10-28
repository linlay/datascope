import { useI18n } from '../ui/preferences';
import { useEffect, useState } from 'react';
import { Alert, Button, Card, Checkbox, Collapse, Form, Input, InputNumber, Modal, Select, Space, Switch, Table, Tag, Typography, message } from 'antd';
import { databases, managementCall, type Database } from '../api';

type Account = { id?: number; database_id: string; username: string; name: string; business_user_id: number; permissions: string[]; active: boolean; revision: number };
export function BusinessAccounts({ db, onSaved }: { db: Database; onSaved: () => void }) {
 const t=useI18n();
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
  try { const authentication = await authForm.validateFields(); setBusy(true); await databases.save({ ...db, authentication }); onSaved(); message.success(t("业务认证来源已保存，请重新授权连接器")); }
  catch (e) { setError((e as Error).message); } finally { setBusy(false); }
 };
 const saveAccount = async () => {
  try {
   const { password, ...values } = await form.validateFields(); setBusy(true);
   await managementCall('save_business_account', { database_id: db.id, account: { ...values, database_id: db.id }, password: password || '', revision: editing?.revision || 0 });
   setEditing(undefined); await load(); message.success(t("业务账号已保存，旧令牌已撤销"));
  } catch (e) { setError((e as Error).message); } finally { setBusy(false); }
 };
 const edit = (account: Account) => { setEditing(account); form.resetFields(); form.setFieldsValue(account); };
 return <Space orientation="vertical" style={{ width: '100%' }}>
  {error && !editing && <Alert type="error" showIcon title={error} />}
  <Collapse items={[{key:'authentication',label:t("业务账号认证设置"),children:<>
   <Typography.Paragraph>{t("业务账号用于当前数据库的 MCP 登录，与平台账号分开管理。")}</Typography.Paragraph>
   <Form form={authForm} layout="vertical">
    <Form.Item name="type" label={t("认证来源")} rules={[{ required: true }]}><Select options={[{ value: 'local', label: t("独立业务账号") }, { value: 'platform', label: t("平台账号（旧配置兼容）") }]} /></Form.Item>
    <Form.Item name="label" label={t("员工登录页名称")}><Input maxLength={40} /></Form.Item>
    <Form.Item name="employee_directory" label={t("同时校验本库 employees 员工目录")} valuePropName="checked"><Switch /></Form.Item>
    <Typography.Paragraph type="secondary">{t("员工目录校验仅用于证券示例库；更换认证来源后需重新授权。")}</Typography.Paragraph>
    <Button loading={busy} onClick={() => void saveAuth()}>{t("保存认证来源")}</Button>
   </Form>
  </>} ]}/>
  {local && <Card title={t("业务用户")} extra={<Button onClick={() => edit({ database_id: db.id, username: '', name: '', business_user_id: 1, permissions: ['query', 'knowledge.read'], active: true, revision: 0 })}>{t("创建业务账号")}</Button>}>
   <Table rowKey="username" dataSource={accounts} columns={[
    { title: t("工号 / 账号"), dataIndex: 'username' }, { title: t("姓名"), dataIndex: 'name' }, { title: t("业务身份"), dataIndex: 'business_user_id' },
    { title: t("状态"), render: (_, a: Account) => <Tag>{a.active ? t("正常") : t("已禁用")}</Tag> },
    { title: t("操作"), render: (_, a: Account) => <Button onClick={() => edit(a)}>{t("编辑 / 重置密码")}</Button> },
   ]} />
  </Card>}
  <Modal title={editing?.revision ? t("编辑业务账号") : t("创建业务账号")} open={!!editing} onCancel={() => setEditing(undefined)} onOk={() => void saveAccount()} confirmLoading={busy}>
   {error && <Alert type="error" showIcon title={error} style={{marginBottom:12}}/>}
   <Form form={form} layout="vertical">
    <Form.Item name="username" label={t("工号 / 账号")} rules={[{ required: true }]}><Input disabled={!!editing?.revision} /></Form.Item>
    <Form.Item name="name" label={t("姓名")} rules={[{ required: true }]}><Input /></Form.Item>
    <Form.Item name="business_user_id" label={t("业务身份编号（用于 SQL 行权限）")} rules={[{ required: true }]}><InputNumber min={1} precision={0} /></Form.Item>
    <Form.Item name="password" label={editing?.revision ? t("新密码（留空保留原密码）") : t("初始密码")} rules={[{ required: !editing?.revision, min: 12 }]}><Input.Password autoComplete="new-password" /></Form.Item>
    <Form.Item name="permissions" label={t("业务能力")}><Checkbox.Group options={[{ value: 'query', label: t("权限查询") }, { value: 'knowledge.read', label: t("读取知识") }, { value: 'raw_query', label: t("全量只读查询（绕过行权限）") }]} /></Form.Item>
    <Form.Item name="active" label={t("启用账号")} valuePropName="checked"><Switch /></Form.Item>
   </Form>
  </Modal>
 </Space>;
}

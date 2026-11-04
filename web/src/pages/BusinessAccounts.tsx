import { useI18n } from '../ui/preferences';
import { useEffect, useState } from 'react';
import { Alert, Button, Checkbox, Form, Input, InputNumber, Modal, Space, Switch, Table, Tag, message } from 'antd';
import { managementCall, type Database } from '../api';

type Account = { id?: number; database_id: string; username: string; name: string; business_user_id: number; permissions: string[]; active: boolean; revision: number };
export function BusinessAccounts({ db }: { db: Database }) {
 const t=useI18n();
 const [accounts, setAccounts] = useState<Account[]>([]);
 const [editing, setEditing] = useState<Account>();
 const [error, setError] = useState<string>();
 const [busy, setBusy] = useState(false);
 const [form] = Form.useForm();
 const load = async () => {
  try { setAccounts(await managementCall<Account[]>('list_business_accounts', { database_id: db.id })); setError(undefined); }
  catch (e) { setError((e as Error).message); }
 };
 useEffect(() => { void load(); }, [db.id]);
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
  <div style={{display:'flex',justifyContent:'flex-end'}}><Button type="primary" onClick={() => edit({ database_id: db.id, username: '', name: '', business_user_id: 1, permissions: ['query', 'knowledge.read'], active: true, revision: 0 })}>{t("创建业务账号")}</Button></div>
   <Table rowKey="username" dataSource={accounts} pagination={false} scroll={{x:660}} columns={[
    { title: t("工号 / 账号"), dataIndex: 'username' }, { title: t("姓名"), dataIndex: 'name' }, { title: t("业务身份"), dataIndex: 'business_user_id' },
    { title: t("状态"), render: (_, a: Account) => <Tag>{a.active ? t("正常") : t("已禁用")}</Tag> },
    { title: t("操作"), render: (_, a: Account) => <Button onClick={() => edit(a)}>{t("编辑 / 重置密码")}</Button> },
   ]} />
  <Modal title={editing?.revision ? t("编辑业务账号") : t("创建业务账号")} open={!!editing} onCancel={() => setEditing(undefined)} onOk={() => void saveAccount()} confirmLoading={busy}>
   {error && <Alert type="error" showIcon title={error} style={{marginBottom:12}}/>}
   <Form form={form} layout="vertical">
    <Form.Item name="username" label={t("工号 / 账号")} rules={[{ required: true }]}><Input disabled={!!editing?.revision} /></Form.Item>
    <Form.Item name="name" label={t("姓名")} rules={[{ required: true }]}><Input /></Form.Item>
    <Form.Item name="business_user_id" label={t("业务身份编号（用于 SQL 行权限）")} rules={[{ required: true }]}><InputNumber min={1} precision={0} /></Form.Item>
    <Form.Item name="password" label={editing?.revision ? t("新密码（留空保留原密码）") : t("初始密码")} rules={[{ required: !editing?.revision, min: 6 }]}><Input.Password autoComplete="new-password" /></Form.Item>
    <Form.Item name="permissions" label={t("业务能力")}><Checkbox.Group options={[{ value: 'action', label: t('执行业务操作') }, { value: 'query', label: t("权限查询") }, { value: 'knowledge.read', label: t("读取知识") }, { value: 'raw_query', label: t("全量只读查询（绕过行权限）") }]} /></Form.Item>
    <Form.Item name="active" label={t("启用账号")} valuePropName="checked"><Switch /></Form.Item>
   </Form>
  </Modal>
 </Space>;
}

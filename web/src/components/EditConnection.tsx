import { useState } from 'react';
import { Alert, Button, Form, Input, InputNumber, Modal, Select, Space, message } from 'antd';
import { managementCall, type Database } from '../api';
import { useI18n } from '../ui/preferences';

export function EditConnection({ db, onSaved }: { db: Database; onSaved: () => void }) {
 const t = useI18n();
 const [form] = Form.useForm();
 const [open, setOpen] = useState(false);
 const [busy, setBusy] = useState(false);
 const [error, setError] = useState<string>();
 const [tested, setTested] = useState(false);
 const [revision, setRevision] = useState(db.revision);
 const close = () => { setOpen(false); form.resetFields(); setError(undefined); setTested(false); };
 const run = async (save: boolean) => {
  const values = await form.validateFields().catch(() => undefined);
  if (!values || busy) return;
  setBusy(true); setError(undefined); setTested(false);
  try {
   await managementCall(save ? 'edit_database_connection' : 'test_edited_database_connection', {
    database_id: db.id, database: { id: db.id, revision, connection: values.connection }, password: values.password,
   });
   if (save) { message.success(t('数据源连接已保存')); close(); onSaved(); } else setTested(true);
  } catch (e) { setError((e as Error).message); } finally { setBusy(false); }
 };
 return <>
  <Button onClick={() => { setRevision(db.revision); form.setFieldsValue({ connection: { ...db.connection, tls_mode: db.connection?.tls_mode || ((db.driver === 'mysql' || db.driver === 'postgresql') ? 'verify-full' : '') }, password: '' }); setOpen(true); }}>{t('编辑连接')}</Button>
  <Modal title={t('编辑连接')} open={open} onCancel={() => { if (!busy) close(); }} footer={<Space>
   <Button disabled={busy} onClick={close}>{t('取消')}</Button>
   <Button loading={busy} onClick={() => void run(false)}>{t('测试连接')}</Button>
   <Button type="primary" loading={busy} onClick={() => void run(true)}>{t('校验并保存')}</Button>
  </Space>}>
   <Alert type="info" showIcon style={{ marginBottom: 16 }} title={t('用于同一业务库的连接维护。密码留空保留原密码；已有写连接同步地址、端口和 TLS。保存前会重新校验连接及已登记字段结构。')} />
   <Form form={form} layout="vertical" disabled={busy} onValuesChange={() => setTested(false)}>
    <Form.Item name={['connection', 'host']} label={t('数据库地址')} rules={[{ required: true, whitespace: true }]}><Input /></Form.Item>
    <Form.Item name={['connection', 'port']} label={t('端口')} rules={[{ required: true }]}><InputNumber min={1} max={65535} precision={0} /></Form.Item>
    <Form.Item name={['connection', 'username']} label={t('数据库只读账号')} rules={[{ required: true, whitespace: true }]}><Input autoComplete="off" /></Form.Item>
    <Form.Item name="password" label={t('数据库密码')}><Input.Password autoComplete="new-password" placeholder={t('留空保留原密码')} /></Form.Item>
    {(db.driver === 'mysql' || db.driver === 'postgresql') && <Form.Item name={['connection', 'tls_mode']} label={t('连接加密')} rules={[{ required: true }]}>
     <Select options={[{ value: 'verify-full', label: t('TLS：校验证书和主机名') }, ...(db.driver === 'postgresql' ? [{ value: 'require', label: t('TLS：只加密') }] : []), { value: 'disable', label: t('关闭 TLS（可信网络）') }]} />
    </Form.Item>}
   </Form>
   {error && <Alert type="error" showIcon title={error} />}
   {tested && <Alert type="success" showIcon title={t('连接及已登记字段结构校验通过')} />}
  </Modal>
 </>;
}

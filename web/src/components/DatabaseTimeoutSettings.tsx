import { useEffect, useState } from 'react';
import { Alert, Card, Form, InputNumber, Typography, message } from 'antd';
import { managementCall, type Database } from '../api';
import { useI18n } from '../ui/preferences';
import { SaveButton } from './IconButton';

export function DatabaseTimeoutSettings({ db, onSaved, disabled }: { db: Database; onSaved: () => void; disabled: boolean }) {
 const t = useI18n();
 const [form] = Form.useForm<{ query_timeout_seconds: number; connect_timeout_seconds: number }>();
 const [busy, setBusy] = useState(false);
 const [error, setError] = useState<string>();
 useEffect(() => {
  form.setFieldsValue({ query_timeout_seconds: db.query_timeout_seconds ?? 0, connect_timeout_seconds: db.connect_timeout_seconds ?? 0 });
  setError(undefined);
 }, [db.id, db.revision, db.query_timeout_seconds, db.connect_timeout_seconds, form]);
 const save = async (values: { query_timeout_seconds: number; connect_timeout_seconds: number }) => {
  setBusy(true); setError(undefined);
  try {
   await managementCall('update_database', { database_id: db.id, database: { id: db.id, revision: db.revision, query_timeout_seconds: values.query_timeout_seconds, ...(db.connection ? { connect_timeout_seconds: values.connect_timeout_seconds } : {}) } });
   message.success(t('超时设置已保存')); onSaved();
  } catch (e) { setError((e as Error).message); }
  finally { setBusy(false); }
 };
 return <Card title={t('超时设置')}>
  {error && <Alert type="error" showIcon title={error} />}
  <Typography.Paragraph type="secondary">{t('填 0 继承服务默认值。查询支持 1–120 秒，连接支持 1–60 秒。保存后新请求生效。')}</Typography.Paragraph>
  <Typography.Paragraph>{t('当前查询上限：{0} 秒', { '0': String(db.effective_query_timeout_seconds ?? 30) })}{db.connection && ` · ${t('当前连接上限：{0} 秒', { '0': String(db.effective_connect_timeout_seconds ?? 10) })}`}</Typography.Paragraph>
  <Form form={form} layout="inline" onFinish={values => void save(values)} disabled={disabled || busy}>
   <Form.Item name="query_timeout_seconds" label={t('查询超时（秒）')} rules={[{ required: true, type: 'integer', min: 0, max: 120, message: t('请输入范围内的整数秒数') }]}><InputNumber aria-label={t('查询超时（秒）')} min={0} max={120} precision={0} /></Form.Item>
   {db.connection && <Form.Item name="connect_timeout_seconds" label={t('连接超时（秒）')} rules={[{ required: true, type: 'integer', min: 0, max: 60, message: t('请输入范围内的整数秒数') }]}><InputNumber aria-label={t('连接超时（秒）')} min={0} max={60} precision={0} /></Form.Item>}
   <Form.Item><SaveButton htmlType="submit" loading={busy} disabled={disabled || busy} /></Form.Item>
  </Form>
 </Card>;
}

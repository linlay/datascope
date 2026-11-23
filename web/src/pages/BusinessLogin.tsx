import { useEffect, useRef, useState } from 'react';
import { Alert, Button, Card, Descriptions, Form, Input, InputNumber, Select, Space, Spin, Tag, Typography, message } from 'antd';
import { CopyOutlined } from '@ant-design/icons';
import { useI18n } from '../ui/preferences';
import { appURL } from '../api';
import { publicRequest } from '../businessLoginRequest';

interface LoginInfo { database_id: string; name: string; mcp_endpoint: string; expires_in: number }
interface Token { access_token: string; expires_in: number; token_type: string }

export default function BusinessLogin({ database }: { database: string }) {
 const t = useI18n();
 const [form] = Form.useForm();
 const lifetime = Form.useWatch('lifetime', form) ?? 3600;
 const durationUnit = Form.useWatch('duration_unit', form) ?? 3600;
 const [info, setInfo] = useState<LoginInfo>();
 const [loading, setLoading] = useState(true);
 const [busy, setBusy] = useState(false);
 const [error, setError] = useState('');
 const [token, setToken] = useState('');
 const [expiresAt, setExpiresAt] = useState(0);
 const [remaining, setRemaining] = useState(0);
 const [expired, setExpired] = useState(false);
 const request = useRef<AbortController>();
 const endpoint = appURL(`/auth/databases/${encodeURIComponent(database)}`);
 useEffect(() => {
  const controller = new AbortController();
  if (!database) { setError(t('请从数据库业务认证页面打开登录链接')); setLoading(false); return; }
  void publicRequest<LoginInfo>(`${endpoint}/login-info`, undefined, controller.signal)
   .then(data => {
    if (data.database_id !== database || typeof data.name !== 'string' || typeof data.mcp_endpoint !== 'string' || !data.mcp_endpoint || !Number.isFinite(data.expires_in) || data.expires_in <= 0) throw new Error('业务登录接口返回无效数据，请检查部署路径和反向代理配置');
    setInfo(data);
   }).catch(e => { if (!controller.signal.aborted) setError(e.message); })
   .finally(() => { if (!controller.signal.aborted) setLoading(false); });
  return () => { controller.abort(); request.current?.abort(); };
 }, [database]);
 useEffect(() => {
  if (!token || !expiresAt) return;
  const tick = () => {
   const left = Math.max(0, Math.ceil((expiresAt - Date.now()) / 1000));
   setRemaining(left);
   if (!left) { setToken(''); setExpired(true); }
  };
  tick();
  const timer = window.setInterval(tick, 1000);
  return () => window.clearInterval(timer);
 }, [token, expiresAt]);
 const login = async (values: { username: string; password: string; lifetime: number | 'custom'; duration?: number; duration_unit?: number }) => {
  if (busy || !info) return;
  request.current?.abort();
  const controller = new AbortController(); request.current = controller;
  setBusy(true); setError(''); setToken(''); setExpired(false);
  try {
   const expiresIn = values.lifetime === 'custom' ? (values.duration || 0) * (values.duration_unit || 3600) : values.lifetime;
   const result = await publicRequest<Token>(`${endpoint}/token`, { username: values.username, password: values.password, resource: info.mcp_endpoint, expires_in: expiresIn }, controller.signal);
   if (!result.access_token || result.token_type !== 'Bearer' || !Number.isSafeInteger(result.expires_in) || result.expires_in < 0) throw new Error(t('服务端返回的 Token 无效'));
   setExpiresAt(result.expires_in === 0 ? 0 : Date.now() + result.expires_in * 1000); setRemaining(result.expires_in); setToken(result.access_token);
  } catch (e) { if (!controller.signal.aborted) setError((e as Error).message); }
  finally { if (!controller.signal.aborted) { form.setFieldValue('password', ''); setBusy(false); } }
 };
 const copyToken = async () => {
  if (expiresAt && Date.now() >= expiresAt) { setToken(''); setExpired(true); return; }
  try { await navigator.clipboard.writeText(token); message.success(t('Token 已复制')); }
  catch { message.error(t('复制失败，请显示 Token 后手动复制')); }
 };
 return <main className="business-login">
  <Card>
   <Typography.Text type="secondary">DataScope · {t('业务登录')}</Typography.Text>
   <Typography.Title level={2}>{info?.name || database || t('业务登录')}</Typography.Title>
   <Typography.Paragraph type="secondary">{t('使用本数据库的业务账号登录，获取个人访问 Token。')}</Typography.Paragraph>
   {loading ? <Spin /> : <>
    {error && <Alert type="error" showIcon title={t(error)} style={{ marginBottom: 16 }} />}
    {expired && <Alert type="warning" showIcon title={t('Token 已过期，请重新登录')} style={{ marginBottom: 16 }} />}
    {info && (token ? <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
     <Alert type="success" showIcon title={t('登录成功')} description={t('连接器选择 Token → Bearer，填写下方个人 Token。')} />
     <Descriptions column={1} items={[
      { key: 'endpoint', label: t('MCP 地址'), children: <Typography.Text copyable className="business-login-address">{info.mcp_endpoint}</Typography.Text> },
      { key: 'expiry', label: t('有效期至'), children: expiresAt ? <Space wrap><span>{new Date(expiresAt).toLocaleString()}</span><Tag>{t('剩余 {0} 分钟', { '0': Math.ceil(remaining / 60) })}</Tag></Space> : <Tag>{t('永久（直到撤销）')}</Tag> },
     ]} />
     <label htmlFor="personal-token">{t('个人 Token')}</label>
     <Input.Password id="personal-token" aria-label={t('个人 Token')} value={token} readOnly autoComplete="off" />
     <Space wrap>
      <Button aria-label={t('复制 Token')} type="primary" icon={<CopyOutlined />} onClick={() => void copyToken()}>{t('复制 Token')}</Button>
      <Button aria-label={t('清除并重新登录')} onClick={() => { setToken(''); setError(''); setExpired(false); }}>{t('清除并重新登录')}</Button>
     </Space>
     <Typography.Text type="secondary">{t('Token 仅保留在当前页面，关闭或刷新后清除；清除页面不会撤销已复制的 Token。')}</Typography.Text>
    </Space> : <Form form={form} layout="vertical" onFinish={login} disabled={busy} initialValues={{ lifetime: 3600, duration: 1, duration_unit: 3600 }}>
     <Form.Item name="username" label={t('用户名')} rules={[{ required: true, message: t('请输入用户名') }]}><Input autoComplete="username" autoFocus /></Form.Item>
     <Form.Item name="password" label={t('密码')} rules={[{ required: true, message: t('请输入密码') }]}><Input.Password autoComplete="current-password" /></Form.Item>
     <Form.Item name="lifetime" label={t('Token 有效期')} rules={[{ required: true }]}><Select options={[
      { value: 3600, label: t('1 小时（默认）') }, { value: 86400, label: t('24 小时') },
      { value: 604800, label: t('7 天') }, { value: 2592000, label: t('30 天') },
      { value: 7776000, label: t('90 天') }, { value: 31536000, label: t('1 年（365 天）') },
      { value: 'custom', label: t('自定义') }, { value: 0, label: t('永久（直到撤销）') },
     ]} /></Form.Item>
     {lifetime === 'custom' && <Space align="start" wrap>
      <Form.Item name="duration" label={t('自定义时长')} dependencies={['duration_unit']} rules={[{ validator: (_, value) => Number.isSafeInteger(value) && value >= 1 && value * durationUnit <= 31536000 ? Promise.resolve() : Promise.reject(new Error(t('Token 有效期须为 1 分钟至 365 天，或选择永久'))) }]}><InputNumber min={1} max={Math.floor(31536000 / durationUnit)} precision={0} /></Form.Item>
      <Form.Item name="duration_unit" label={t('时长单位')}><Select style={{ width: 100 }} options={[{ value: 60, label: t('分钟') }, { value: 3600, label: t('小时') }, { value: 86400, label: t('天') }]} /></Form.Item>
     </Space>}
     {lifetime === 0 && <Alert style={{ marginBottom: 16 }} type="info" showIcon title={t('永久 Token 不按时间过期；本地账号重置密码、禁用或修改权限后，旧 Token 仍会失效。')} />}
     <Button aria-label={t('登录并获取 Token')} type="primary" htmlType="submit" block loading={busy}>{t('登录并获取 Token')}</Button>
     <Typography.Paragraph type="secondary" style={{ marginTop: 16 }}>{t('Token 默认有效期为 1 小时，可选择其他时长或永久，绑定当前业务账号及其权限。')}</Typography.Paragraph>
    </Form>)}
   </>}
  </Card>
 </main>;
}

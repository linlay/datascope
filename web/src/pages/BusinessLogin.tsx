import { useEffect, useRef, useState } from 'react';
import { Alert, Button, Card, Descriptions, Form, Input, Space, Spin, Tag, Typography, message } from 'antd';
import { CopyOutlined } from '@ant-design/icons';
import { useI18n } from '../ui/preferences';

interface LoginInfo { database_id: string; name: string; mcp_endpoint: string; expires_in: number }
interface Token { access_token: string; expires_in: number; token_type: string }

// Public business authentication must not redirect through platform login or
// persist passwords/tokens in browser storage, URLs, or platform sessions.
async function publicRequest<T>(path: string, body?: unknown, signal?: AbortSignal): Promise<T> {
 const response = await fetch(path, {
  method: body === undefined ? 'GET' : 'POST', credentials: 'omit', cache: 'no-store', signal,
  headers: { 'Content-Type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body),
 });
 const data = await response.json().catch(() => null);
 if (!response.ok) throw new Error(data?.error || (response.status === 429 ? '登录过于频繁，请稍后再试' : '暂时无法登录，请稍后重试'));
 return data as T;
}

export default function BusinessLogin({ database }: { database: string }) {
 const t = useI18n();
 const [form] = Form.useForm();
 const [info, setInfo] = useState<LoginInfo>();
 const [loading, setLoading] = useState(true);
 const [busy, setBusy] = useState(false);
 const [error, setError] = useState('');
 const [token, setToken] = useState('');
 const [expiresAt, setExpiresAt] = useState(0);
 const [remaining, setRemaining] = useState(0);
 const [expired, setExpired] = useState(false);
 const request = useRef<AbortController>();
 const endpoint = `/auth/databases/${encodeURIComponent(database)}`;
 useEffect(() => {
  const controller = new AbortController();
  if (!database) { setError(t('请从数据库业务认证页面打开登录链接')); setLoading(false); return; }
  void publicRequest<LoginInfo>(`${endpoint}/login-info`, undefined, controller.signal)
   .then(setInfo).catch(e => { if (!controller.signal.aborted) setError(e.message); })
   .finally(() => { if (!controller.signal.aborted) setLoading(false); });
  return () => { controller.abort(); request.current?.abort(); };
 }, [database]);
 useEffect(() => {
  if (!token) return;
  const tick = () => {
   const left = Math.max(0, Math.ceil((expiresAt - Date.now()) / 1000));
   setRemaining(left);
   if (!left) { setToken(''); setExpired(true); }
  };
  tick();
  const timer = window.setInterval(tick, 1000);
  return () => window.clearInterval(timer);
 }, [token, expiresAt]);
 const login = async (values: { username: string; password: string }) => {
  if (busy || !info) return;
  request.current?.abort();
  const controller = new AbortController(); request.current = controller;
  setBusy(true); setError(''); setToken(''); setExpired(false);
  try {
   const result = await publicRequest<Token>(`${endpoint}/token`, { ...values, resource: info.mcp_endpoint }, controller.signal);
   if (!result.access_token || result.token_type !== 'Bearer' || !Number.isFinite(result.expires_in) || result.expires_in <= 0) throw new Error(t('服务端返回的 Token 无效'));
   setExpiresAt(Date.now() + result.expires_in * 1000); setRemaining(result.expires_in); setToken(result.access_token);
  } catch (e) { if (!controller.signal.aborted) setError((e as Error).message); }
  finally { if (!controller.signal.aborted) { form.setFieldValue('password', ''); setBusy(false); } }
 };
 const copyToken = async () => {
  if (Date.now() >= expiresAt) { setToken(''); setExpired(true); return; }
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
      { key: 'expiry', label: t('有效期至'), children: <Space wrap><span>{new Date(expiresAt).toLocaleString()}</span><Tag>{t('剩余 {0} 分钟', { '0': Math.ceil(remaining / 60) })}</Tag></Space> },
     ]} />
     <label htmlFor="personal-token">{t('个人 Token')}</label>
     <Input.Password id="personal-token" aria-label={t('个人 Token')} value={token} readOnly autoComplete="off" />
     <Space wrap>
      <Button aria-label={t('复制 Token')} type="primary" icon={<CopyOutlined />} onClick={() => void copyToken()}>{t('复制 Token')}</Button>
      <Button aria-label={t('清除并重新登录')} onClick={() => { setToken(''); setError(''); setExpired(false); }}>{t('清除并重新登录')}</Button>
     </Space>
     <Typography.Text type="secondary">{t('Token 仅保留在当前页面，关闭或刷新后清除；清除页面不会撤销已复制的 Token。')}</Typography.Text>
    </Space> : <Form form={form} layout="vertical" onFinish={login} disabled={busy}>
     <Form.Item name="username" label={t('用户名')} rules={[{ required: true, message: t('请输入用户名') }]}><Input autoComplete="username" autoFocus /></Form.Item>
     <Form.Item name="password" label={t('密码')} rules={[{ required: true, message: t('请输入密码') }]}><Input.Password autoComplete="current-password" /></Form.Item>
     <Button aria-label={t('登录并获取 Token')} type="primary" htmlType="submit" block loading={busy}>{t('登录并获取 Token')}</Button>
     <Typography.Paragraph type="secondary" style={{ marginTop: 16 }}>{t('Token 有效期为 1 小时，绑定当前业务账号及其权限。')}</Typography.Paragraph>
    </Form>)}
   </>}
  </Card>
 </main>;
}

import { useEffect, useState } from 'react';
import { Alert, Button, Card, Form, Input, InputNumber, Modal, Radio, Select, Space, Switch, Checkbox, Typography, message } from 'antd';
import { databases, managementCall, type AuthConfig, type AuthPolicy, type AuthProvider, type Database } from '../api';
import { useAuth } from '../Auth';
import { useI18n } from '../ui/preferences';
import { BusinessAccounts } from '../pages/BusinessAccounts';

type Authentication = NonNullable<Database['authentication']>;
const providerTypes = (mode: AuthPolicy['mode']) => mode === 'OAUTH' ? ['DEMO_OAUTH', 'ENTERPRISE_OAUTH'] : ['BUSINESS_ACCOUNT', 'ENTERPRISE_ACCOUNT_API'];
const oauthFields = [
 ['issuer', '认证服务标识（Issuer）'], ['authorize_url', '登录授权地址'], ['token_url', 'Token 地址'], ['userinfo_url', '用户信息地址'], ['client_id', '应用标识（Client ID）'],
] as const;
const accountFields = [['account_url', '账号认证接口']] as const;
const optionalFields = [
 ['secret_env', '密钥环境变量'], ['subject_claim', '用户唯一标识字段'], ['username_claim', '账号名称字段（可选）'], ['business_id_claim', '业务员工编号字段'],
] as const;

export function DatabaseAuthentication({ db, onSaved }: { db: Database; onSaved: () => void }) {
 const t = useI18n();
 const { user } = useAuth();
 const [optionsOpen, setOptionsOpen] = useState(false);
 const [busy, setBusy] = useState(false);
 const [error, setError] = useState('');
 const [form] = Form.useForm();
 const [providers, setProviders] = useState<AuthConfig['providers']>([]);
 const [draft, setDraft] = useState<Authentication>();
 const current: Authentication = draft || db.authentication || { type: 'local' };
 const selected = current.access || db.access?.policy || { id: 'database-auth', name: '本库业务认证', mode: 'OAUTH' as const, permissions: ['query'] };
 const mode = selected.mode;
 const source = current.source || 'GLOBAL';
 const available = providers.filter(p => p.enabled && providerTypes(mode).includes(p.type));
 const selectedProvider = source === 'DATABASE' ? current.provider : providers.find(p => p.id === selected.provider_id) || (db.access?.provider.id === selected.provider_id ? db.access?.provider : undefined);
 const usesAccounts = mode === 'ACCOUNT' && selectedProvider?.type === 'BUSINESS_ACCOUNT' || mode === 'OAUTH' && selectedProvider?.type === 'DEMO_OAUTH';
 const enterprise = selectedProvider?.type === 'ENTERPRISE_OAUTH' || selectedProvider?.type === 'ENTERPRISE_ACCOUNT_API';
 const dirty = !!draft;
 const savedLocal = ['DEMO_OAUTH', 'BUSINESS_ACCOUNT'].includes(db.access?.provider.type || '');
 useEffect(() => { if (!user.system_admin) return; let live = true; managementCall<AuthConfig>('get_auth_config').then(cfg => { if (live) setProviders(cfg.providers); }).catch(e => { if (live) setError(e.message); }); return () => { live = false; }; }, [user.system_admin, db.id]);
 useEffect(() => { setDraft(undefined); setOptionsOpen(false); setError(''); }, [db.id, db.revision]);
 const newProvider = (nextMode: AuthPolicy['mode'], type = providerTypes(nextMode)[1]): AuthProvider => ({ id: 'database-provider', name: db.name + '业务认证', type, enabled: true, subject_claim: 'sub', business_id_claim: 'employee_id' });
 const globalProvider = (nextMode: AuthPolicy['mode']) => providers.find(p => p.enabled && providerTypes(nextMode).includes(p.type) && p.id === selected.provider_id) || providers.find(p => p.enabled && p.type === providerTypes(nextMode)[0]) || providers.find(p => p.enabled && providerTypes(nextMode).includes(p.type));
 const update = (patch: Partial<Authentication>) => { setDraft({ ...current, access: { ...selected }, ...patch }); setError(''); };
 const patchPolicy = (patch: Partial<AuthPolicy>) => update({ access: { ...selected, ...patch } });
 const changeMode = (nextMode: AuthPolicy['mode']) => {
  const provider = nextMode === 'NONE' ? undefined : source === 'DATABASE' ? (current.provider && providerTypes(nextMode).includes(current.provider.type) ? current.provider : newProvider(nextMode)) : globalProvider(nextMode);
  update({ source: nextMode === 'NONE' ? 'GLOBAL' : source, provider: source === 'DATABASE' && nextMode !== 'NONE' ? provider : undefined,
   access: { id: 'database-auth', name: '本库业务认证', mode: nextMode, provider_id: provider?.id, permissions: nextMode === 'NONE' ? ['query'] : selected.permissions || ['query'], business_user_id: nextMode === 'NONE' ? selected.business_user_id || 0 : undefined } });
 };
 const changeSource = (nextSource: 'GLOBAL' | 'DATABASE') => {
  const provider = nextSource === 'DATABASE' ? newProvider(mode) : globalProvider(mode);
  update({ source: nextSource, provider: nextSource === 'DATABASE' ? provider : undefined, access: { ...selected, provider_id: provider?.id } });
 };
 const patchProvider = (patch: Partial<AuthProvider>) => update({ provider: { ...current.provider!, ...patch } });
 const saveMode = async () => {
  if (!draft) return;
  try {
   if (source === 'DATABASE' && enterprise) {
    for (const [key, label] of selectedProvider?.type === 'ENTERPRISE_OAUTH' ? oauthFields : accountFields) {
     if (!selectedProvider?.[key]?.trim()) throw new Error(t('{0}不能为空', { '0': t(label) }));
    }
   }
   setBusy(true); setError('');
   await databases.save({ ...db, authentication: { ...current, source: mode === 'NONE' ? 'GLOBAL' : source, provider: source === 'DATABASE' && mode !== 'NONE' ? current.provider : undefined, access: { ...selected, id: 'database-auth', name: '本库业务认证' } } });
   setDraft(undefined); onSaved(); message.success(t('认证方式已保存，业务客户端需重新连接'));
  } catch (e) { setError((e as Error).message); } finally { setBusy(false); }
 };
 const openOptions = () => { form.setFieldsValue(db.authentication || { label: '', employee_directory: false }); setError(''); setOptionsOpen(true); };
 const saveOptions = async () => {
  try {
   const values = await form.validateFields(); setBusy(true); setError('');
   await databases.save({ ...db, authentication: { ...db.authentication, ...values, type: 'local' } });
   setOptionsOpen(false); onSaved(); message.success(t('业务认证设置已保存，请重新授权连接器'));
  } catch (e) { setError((e as Error).message); } finally { setBusy(false); }
 };
 const endpoint = new URL(db.mcp_endpoint || `/mcp/${db.mcp_path || db.id}`, window.location.origin);
 const metadataURL = `${endpoint.origin}/.well-known/oauth-protected-resource${endpoint.pathname}`;
 return <>
  <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
   <Card title={t('业务认证')} extra={user.system_admin ? <Space><Button disabled={!dirty || busy} onClick={() => { setDraft(undefined); setError(''); }}>{t('取消')}</Button><Button type="primary" loading={busy} disabled={!dirty || mode !== 'NONE' && !selectedProvider?.enabled} onClick={() => void saveMode()}>{t('保存')}</Button></Space> : undefined}>
    <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
     {error && !optionsOpen && <Alert type="error" showIcon title={error} />}
     <Radio.Group aria-label={t('认证方式')} value={mode} disabled={!user.system_admin || busy} onChange={e => changeMode(e.target.value)} options={[{ value: 'NONE', label: t('无需认证') }, { value: 'ACCOUNT', label: t('账号密码（Token）') }, { value: 'OAUTH', label: 'OAuth' }]} />
     {mode !== 'NONE' && <>
      <Space wrap><Typography.Text>{t('配置来源')}</Typography.Text><Radio.Group aria-label={t('配置来源')} value={source} disabled={!user.system_admin || busy} onChange={e => changeSource(e.target.value)} options={[{ value: 'GLOBAL', label: t('使用全局提供方') }, { value: 'DATABASE', label: t('本库独立配置') }]} /></Space>
      {source === 'GLOBAL' ? <Space wrap>
       <Typography.Text>{t('认证提供方')}</Typography.Text>
       <Select aria-label={t('认证提供方')} style={{ width: 280, maxWidth: '100%' }} disabled={!user.system_admin || busy} value={selected.provider_id} options={user.system_admin ? available.map(p => ({ value: p.id, label: ['BUSINESS_ACCOUNT', 'DEMO_OAUTH'].includes(p.type) ? t('本库业务账号') + (p.type === 'DEMO_OAUTH' ? ' · Demo OAuth' : '') : p.name })) : [{ value: selected.provider_id, label: db.access?.provider.name }]} onChange={provider_id => patchPolicy({ provider_id })} />
      </Space> : <>
       <Space wrap><Typography.Text>{t('认证服务')}</Typography.Text><Select aria-label={t('认证服务')} style={{ width: 280, maxWidth: '100%' }} disabled={!user.system_admin || busy} value={selectedProvider?.type} options={providerTypes(mode).map(type => ({ value: type, label: type === 'ENTERPRISE_OAUTH' ? t('企业 OAuth') : type === 'ENTERPRISE_ACCOUNT_API' ? t('企业账号接口') : type === 'DEMO_OAUTH' ? t('本地 OAuth') : t('本库业务账号') }))} onChange={type => { const provider = newProvider(mode, type); update({ provider, access: { ...selected, provider_id: provider.id } }); }} /></Space>
       {enterprise && <Form layout="vertical" disabled={!user.system_admin || busy} className="auth-connection-form">
        <div className="auth-connection-fields">
         {(selectedProvider?.type === 'ENTERPRISE_OAUTH' ? oauthFields : accountFields).map(([key, label]) => <Form.Item key={key} label={t(label)} required><Input aria-label={t(label)} value={selectedProvider?.[key] || ''} onChange={e => patchProvider({ [key]: e.target.value })} placeholder={key === 'client_id' ? 'datascope' : 'https://identity.example.com/…'} /></Form.Item>)}
         {optionalFields.map(([key, label]) => <Form.Item key={key} label={t(label)} tooltip={key === 'secret_env' ? t('填写服务端环境变量名，不在配置中保存密钥明文。') : key === 'business_id_claim' ? t('认证结果中映射到 SQL 行权限的正整数员工编号字段。') : undefined}><Input aria-label={t(label)} value={selectedProvider?.[key] || ''} onChange={e => patchProvider({ [key]: e.target.value })} placeholder={key === 'secret_env' ? 'DATASCOPE_AUTH_ENTERPRISE_SECRET' : undefined} /></Form.Item>)}
        </div>
       </Form>}
      </>}
     </>}
     {(mode === 'NONE' || !usesAccounts) && <Space wrap>
      {mode === 'NONE' && <><Typography.Text>{t('匿名业务身份')}</Typography.Text><InputNumber aria-label={t('匿名业务身份')} min={0} precision={0} disabled={!user.system_admin || busy} value={selected.business_user_id || 0} onChange={value => patchPolicy({ business_user_id: value || 0 })} /></>}
      <Checkbox.Group disabled={!user.system_admin || busy} value={selected.permissions || []} options={[{ value: 'query', label: t('查询') }, { value: 'knowledge.read', label: t('知识查询') }, { value: 'raw_query', label: t('原始查询') }, { value: 'action', label: t('执行动作') }]} onChange={permissions => patchPolicy({ permissions: permissions as string[] })} />
     </Space>}
     {usesAccounts && user.system_admin && <Button disabled={busy || dirty || !savedLocal} onClick={openOptions}>{t('登录选项')}</Button>}
     {db.access?.policy.mode === 'OAUTH' && <div><Typography.Text type="secondary" style={{ marginInlineEnd: 8 }}>{t('已生效的发现地址')}</Typography.Text><Typography.Link href={metadataURL} target="_blank" rel="noreferrer" style={{ overflowWrap: 'anywhere' }}>{metadataURL}</Typography.Link></div>}
    </Space>
   </Card>
   {usesAccounts && user.system_admin && <Card title={t('本地账号')}><BusinessAccounts key={db.id} db={db}/></Card>}
  </Space>
  <Modal title={t('登录选项')} open={optionsOpen} onCancel={() => setOptionsOpen(false)} onOk={() => void saveOptions()} confirmLoading={busy} destroyOnHidden>
   {error && <Alert type="error" showIcon title={error} style={{ marginBottom: 16 }} />}
   <Form form={form} layout="vertical">
    <Form.Item name="label" label={t('登录页名称')}><Input maxLength={40} placeholder={db.name} /></Form.Item>
    {db.driver === 'sqlite' && <Form.Item name="employee_directory" label={t('校验演示库员工目录')} valuePropName="checked" tooltip={t('仅用于证券演示库，校验工号、员工编号和在职状态。')}><Switch /></Form.Item>}
   </Form>
  </Modal>
 </>;
}

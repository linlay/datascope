import { SaveButton, useSaveModalProps } from './IconButton';
import { useEffect, useState } from 'react';
import { Alert, Button, Card, Form, Input, InputNumber, Modal, Radio, Select, Space, Switch, Checkbox, Typography, message } from 'antd';
import { databases, managementCall, type AuthConfig, type AuthPolicy, type AuthProvider, type Database } from '../api';
import { useAuth } from '../Auth';
import { useI18n } from '../ui/preferences';
import { OneIDKeyInput, IdentityMappingsInput } from './OneIDConfiguration';
import { BusinessAccounts } from '../pages/BusinessAccounts';

type Authentication = NonNullable<Database['authentication']>;
const providerTypes = (mode: AuthPolicy['mode']) => mode === 'OAUTH' ? ['DEMO_OAUTH', 'ENTERPRISE_OAUTH'] : mode === 'ONEID' ? ['ONEID'] : mode === 'TOKEN' ? ['STATIC_TOKEN', 'BUSINESS_ACCOUNT', 'ENTERPRISE_ACCOUNT_API'] : ['BUSINESS_ACCOUNT', 'ENTERPRISE_ACCOUNT_API'];
const providerLabel = (type: string) => ({ STATIC_TOKEN: 'Token / API Key', ONEID: '企业 OneID（客户端 ID Token）', BUSINESS_ACCOUNT: '账号密码 / 业务 Token', ENTERPRISE_ACCOUNT_API: '企业账号接口', DEMO_OAUTH: '本库业务账号 · OAuth', ENTERPRISE_OAUTH: '企业 SSO（跳转登录）' }[type] || type);
const providerMode = (type: string): AuthPolicy['mode'] => ['BUSINESS_ACCOUNT', 'ENTERPRISE_ACCOUNT_API'].includes(type) ? 'ACCOUNT' : type === 'STATIC_TOKEN' ? 'TOKEN' : type === 'ONEID' ? 'ONEID' : 'OAUTH';
const oneIDFields = [['issuer', '身份签发方（Issuer）'], ['audience', '接收方（Audience）']] as const;
const oauthFields = [
 ['issuer', '认证服务标识（Issuer）'], ['authorize_url', '登录授权地址'], ['token_url', 'Token 地址'], ['userinfo_url', '用户信息地址'], ['client_id', '应用标识（Client ID）'],
] as const;
const accountFields = [['account_url', '账号认证接口']] as const;
const optionalFields = [
 ['secret_env', '密钥环境变量'], ['subject_claim', '用户唯一标识字段'], ['username_claim', '账号名称字段（可选）'], ['business_id_claim', '业务员工编号字段'],
] as const;

export function DatabaseAuthentication({ db, onSaved }: { db: Database; onSaved: () => void }) {
 const saveModalProps = useSaveModalProps();
 const t = useI18n();
 const { user } = useAuth();
 const [optionsOpen, setOptionsOpen] = useState(false);
 const [issuedKey,setIssuedKey]=useState('');
 const [busy, setBusy] = useState(false);
 const [error, setError] = useState('');
 const [form] = Form.useForm();
 const [providers, setProviders] = useState<AuthConfig['providers']>([]);
 const [draft, setDraft] = useState<Authentication>();
 const current: Authentication = draft || db.authentication || { type: 'local' };
 const selected = current.access || db.access?.policy || { id: 'database-auth', name: '本库业务认证', mode: 'OAUTH' as const, permissions: ['query'] };
 const mode = selected.mode;
 const source = current.source || 'GLOBAL';
 const displayMode = mode === 'ACCOUNT' ? 'TOKEN' : mode;
 const available = providers.filter(p => p.enabled && providerTypes(displayMode).includes(p.type));
 const selectedProvider = source === 'DATABASE' ? current.provider : providers.find(p => p.id === selected.provider_id) || (db.access?.provider.id === selected.provider_id ? db.access?.provider : undefined);
 const usesAccounts = mode === 'ACCOUNT' && selectedProvider?.type === 'BUSINESS_ACCOUNT' || mode === 'OAUTH' && selectedProvider?.type === 'DEMO_OAUTH';
 const enterprise = selectedProvider?.type === 'ENTERPRISE_OAUTH' || selectedProvider?.type === 'ENTERPRISE_ACCOUNT_API';
 const dirty = !!draft;
 const savedLocal = ['DEMO_OAUTH', 'BUSINESS_ACCOUNT'].includes(db.access?.provider.type || '');
 useEffect(() => { if (!user.system_admin) return; let live = true; managementCall<AuthConfig>('get_auth_config').then(cfg => { if (live) setProviders(cfg.providers); }).catch(e => { if (live) setError(e.message); }); return () => { live = false; }; }, [user.system_admin, db.id]);
 useEffect(() => { setDraft(undefined); setOptionsOpen(false); setError(''); }, [db.id, db.revision]);
 useEffect(()=>setIssuedKey(''),[db.id]);
 const newProvider = (nextMode: AuthPolicy['mode'], type = nextMode === 'OAUTH' ? 'DEMO_OAUTH' : providerTypes(nextMode)[0]): AuthProvider => ({ id: 'database-provider', name: db.name + '业务认证', type, enabled: true, subject_claim: 'sub', business_id_claim: 'employee_id' });
 const globalProvider = (nextMode: AuthPolicy['mode']) => providers.find(p => p.enabled && providerTypes(nextMode).includes(p.type) && p.id === selected.provider_id) || providers.find(p => p.enabled && p.type === providerTypes(nextMode)[0]) || providers.find(p => p.enabled && providerTypes(nextMode).includes(p.type));
 const update = (patch: Partial<Authentication>) => { setDraft({ ...current, access: { ...selected }, ...patch }); setError(''); };
 const patchPolicy = (patch: Partial<AuthPolicy>) => update({ access: { ...selected, ...patch } });
 const changeMode = (nextMode: AuthPolicy['mode']) => {
  let nextSource = source;
  let provider = nextMode === 'NONE' ? undefined : source === 'DATABASE' ? (current.provider && providerTypes(nextMode).includes(current.provider.type) ? current.provider : newProvider(nextMode)) : globalProvider(nextMode);
  if (nextMode !== 'NONE' && !provider) { nextSource = 'DATABASE'; provider = newProvider(nextMode); }
  const policyMode = provider ? providerMode(provider.type) : 'NONE';
  update({ source: nextMode === 'NONE' ? 'GLOBAL' : nextSource, provider: nextSource === 'DATABASE' && nextMode !== 'NONE' ? provider : undefined,
   access: { id: 'database-auth', name: '本库业务认证', mode: policyMode, provider_id: provider?.id, permissions: selected.permissions || ['query'], business_user_id: ['NONE', 'TOKEN'].includes(policyMode) ? selected.business_user_id || 0 : undefined } });
 };
 const changeSource = (nextSource: 'GLOBAL' | 'DATABASE') => {
  const provider = nextSource === 'DATABASE' ? newProvider(displayMode) : globalProvider(displayMode);
  update({ source: nextSource, provider: nextSource === 'DATABASE' ? provider : undefined, access: { ...selected, mode: provider ? providerMode(provider.type) : mode, provider_id: provider?.id } });
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
   if (mode === 'ONEID') for (const [key, label] of oneIDFields) { if (!selectedProvider?.[key]?.trim()) throw new Error(t('{0}不能为空', { '0': t(label) })); }
   if (mode === 'ONEID' && (!selectedProvider?.jwks_url?.trim() === !selectedProvider?.public_key_pem?.trim())) throw new Error(t('JWKS 地址与 PEM 公钥必须且只能配置一种'));
   if (mode === 'TOKEN' && !selected.business_user_id) throw new Error(t('请填写正整数业务身份'));
   setBusy(true); setError('');
   await databases.save({ ...db, authentication: { ...current, source: mode === 'NONE' ? 'GLOBAL' : source, provider: source === 'DATABASE' && mode !== 'NONE' ? current.provider : undefined, access: { ...selected, id: 'database-auth', name: '本库业务认证' } } });
   setDraft(undefined); onSaved(); message.success(t('认证方式已保存，业务客户端需重新连接'));
  } catch (e) { setError((e as Error).message); } finally { setBusy(false); }
 };
 const manageKey = async (action: 'rotate_database_api_key' | 'revoke_database_api_key') => {
  setBusy(true);setError('');setIssuedKey('');
  try {
   const result=await managementCall<{api_key?:string}>(action,{database_id:db.id});
   onSaved();
   if(result.api_key)setIssuedKey(result.api_key);
   else message.success(t('API Key 已撤销'));
  }catch(e){setError((e as Error).message);}finally{setBusy(false);}
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
   <Card title={t('业务认证')} extra={user.system_admin ? <Space><Button disabled={!dirty || busy} onClick={() => { setDraft(undefined); setError(''); }}>{t('取消')}</Button><SaveButton type="primary" loading={busy} disabled={!dirty || mode !== 'NONE' && !selectedProvider?.enabled} onClick={() => void saveMode()} /></Space> : undefined}>
    <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
     {error && !optionsOpen && <Alert type="error" showIcon title={error} />}
     <Radio.Group aria-label={t('认证方式')} value={displayMode} disabled={!user.system_admin || busy} onChange={e => changeMode(e.target.value)} options={[{ value: 'NONE', label: t('无需认证（none）') }, { value: 'OAUTH', label: t('SSO / MCP OAuth（mcp）') }, { value: 'ONEID', label: t('企业 OneID（oneid）') }, { value: 'TOKEN', label: 'Token / API Key（token）' }]} />
     {mode !== 'NONE' && <>
      <Space wrap><Typography.Text>{t('配置来源')}</Typography.Text><Radio.Group aria-label={t('配置来源')} value={source} disabled={!user.system_admin || busy} onChange={e => changeSource(e.target.value)} options={[{ value: 'GLOBAL', label: t('使用全局提供方') }, { value: 'DATABASE', label: t('本库独立配置') }]} /></Space>
      {source === 'GLOBAL' ? <Space wrap>
       <Typography.Text>{t('认证提供方')}</Typography.Text>
       <Select aria-label={t('认证提供方')} style={{ width: 280, maxWidth: '100%' }} disabled={!user.system_admin || busy} value={selected.provider_id} options={user.system_admin ? available.map(p => ({ value: p.id, label: `${p.name} · ${t(providerLabel(p.type))}` })) : [{ value: selected.provider_id, label: db.access?.provider.name }]} onChange={provider_id => { const p = providers.find(p => p.id === provider_id); patchPolicy({ provider_id, mode: p ? providerMode(p.type) : mode }); }} />
      </Space> : <>
       <Space wrap><Typography.Text>{t('认证服务')}</Typography.Text><Select aria-label={t('认证服务')} style={{ width: 280, maxWidth: '100%' }} disabled={!user.system_admin || busy} value={selectedProvider?.type} options={providerTypes(displayMode).map(type => ({ value: type, label: t(providerLabel(type)) }))} onChange={type => { const provider = newProvider(displayMode, type); update({ provider, access: { ...selected, mode: providerMode(type), provider_id: provider.id } }); }} /></Space>
       {(mode === 'ONEID' || mode === 'TOKEN') && <Form layout="vertical" disabled={!user.system_admin || busy} className="auth-connection-form">
        {(mode === 'ONEID' ? oneIDFields : []).map(([key, label]) => <Form.Item key={key} label={t(label)} required><Input aria-label={t(label)} value={selectedProvider?.[key] || ''} onChange={e => patchProvider({ [key]: e.target.value })} /></Form.Item>)}
        {mode === 'ONEID' && <OneIDKeyInput jwksUrl={selectedProvider?.jwks_url} publicKeyPem={selectedProvider?.public_key_pem} disabled={!user.system_admin || busy} onChange={patchProvider} />}
        {mode === 'TOKEN' && <Form.Item label={t('API Key 请求头（留空使用 Bearer）')}><Input aria-label={t('API Key 请求头')} value={selectedProvider?.header_name || ''} onChange={e => patchProvider({ header_name: e.target.value })} placeholder="X-API-Key" /></Form.Item>}
        {mode === 'ONEID' && optionalFields.filter(([key]) => key !== 'secret_env').map(([key,label]) => <Form.Item key={key} label={t(label)}><Input aria-label={t(label)} value={selectedProvider?.[key] || ''} onChange={e => patchProvider({ [key]: e.target.value })} /></Form.Item>)}
       </Form>}
       {enterprise && <Form layout="vertical" disabled={!user.system_admin || busy} className="auth-connection-form">
        <div className="auth-connection-fields">
         {(selectedProvider?.type === 'ENTERPRISE_OAUTH' ? oauthFields : accountFields).map(([key, label]) => <Form.Item key={key} label={t(label)} required><Input aria-label={t(label)} value={selectedProvider?.[key] || ''} onChange={e => patchProvider({ [key]: e.target.value })} placeholder={key === 'client_id' ? 'datascope' : 'https://identity.example.com/…'} /></Form.Item>)}
         {optionalFields.map(([key, label]) => <Form.Item key={key} label={t(label)} tooltip={key === 'secret_env' ? t('填写服务端环境变量名，不在配置中保存密钥明文。') : key === 'business_id_claim' ? t('认证结果中映射到 SQL 行权限的正整数员工编号字段。') : undefined}><Input aria-label={t(label)} value={selectedProvider?.[key] || ''} onChange={e => patchProvider({ [key]: e.target.value })} placeholder={key === 'secret_env' ? 'DATASCOPE_AUTH_ENTERPRISE_SECRET' : undefined} /></Form.Item>)}
        </div>
       </Form>}
      </>}
     </>}
     {mode === 'TOKEN' && <Space orientation="vertical">
      <Alert type="info" showIcon title={t('API Key 仅在生成时显示。保存本库配置后可生成或重置密钥；重置后旧密钥立即失效。')} />
      {source==='DATABASE' && user.system_admin && <Space wrap>
       <Button disabled={dirty||busy||db.access?.policy.mode!=='TOKEN'} loading={busy} onClick={()=>Modal.confirm({title:t('生成或重置 API Key'),content:t('旧 API Key 将立即失效，请同步更新连接器。'),onOk:()=>manageKey('rotate_database_api_key')})}>{t('生成或重置 API Key')}</Button>
       <Button danger disabled={dirty||busy||db.access?.policy.mode!=='TOKEN'} onClick={()=>Modal.confirm({title:t('撤销 API Key'),content:t('撤销后，该密钥不能继续访问本数据库。'),onOk:()=>manageKey('revoke_database_api_key')})}>{t('撤销 API Key')}</Button>
      </Space>}
     </Space>}
     {mode === 'ONEID' && <Form layout="vertical"><Form.Item label={t('本库业务身份映射')}><IdentityMappingsInput value={selected.business_id_mappings} disabled={!user.system_admin || busy} onChange={business_id_mappings => patchPolicy({ business_id_mappings })} /></Form.Item></Form>}
     {mode === 'OAUTH' && <Alert type="info" showIcon title={t(selectedProvider?.type === 'ENTERPRISE_OAUTH' ? 'Platform 选择 mcp，通过授权发现跳转到企业 SSO 登录，取得访问令牌后访问本库。' : 'Platform 选择 mcp，通过 OAuth 跳转到本库业务账号登录。企业 SSO 请使用企业 SSO 提供方。')} />}
     {mode === 'ONEID' && <Alert type="info" showIcon title={t('Platform 选择 oneid，客户端直接发送自身的 ID Token，无需为本库跳转 SSO 登录。服务端验签并映射业务身份；姓名默认读取 name。')} />}
     {(mode === 'NONE' || !usesAccounts) && <Space wrap>
      {(mode === 'NONE' || mode === 'TOKEN') && <><Typography.Text>{t(mode === 'TOKEN' ? 'Token 业务身份' : '匿名业务身份')}</Typography.Text><InputNumber aria-label={t(mode === 'TOKEN' ? 'Token 业务身份' : '匿名业务身份')} min={0} precision={0} disabled={!user.system_admin || busy} value={selected.business_user_id || 0} onChange={value => patchPolicy({ business_user_id: value || 0 })} /></>}
      <Checkbox.Group disabled={!user.system_admin || busy} value={selected.permissions || []} options={[{ value: 'query', label: t('查询') }, { value: 'knowledge.read', label: t('知识查询') }, { value: 'raw_query', label: t('原始查询') }, { value: 'action', label: t('执行动作') }]} onChange={permissions => patchPolicy({ permissions: permissions as string[] })} />
     </Space>}
     {usesAccounts && user.system_admin && <Button disabled={busy || dirty || !savedLocal} onClick={openOptions}>{t('登录选项')}</Button>}
     {db.access?.policy.mode === 'ACCOUNT' && db.mcp_enabled && <Button href={`#/business-login?database=${encodeURIComponent(db.id)}`} target="_blank" rel="noreferrer">{t('登录并获取个人 Token')}</Button>}
     {db.access?.policy.mode === 'OAUTH' && <div><Typography.Text type="secondary" style={{ marginInlineEnd: 8 }}>{t('已生效的发现地址')}</Typography.Text><Typography.Link href={metadataURL} target="_blank" rel="noreferrer" style={{ overflowWrap: 'anywhere' }}>{metadataURL}</Typography.Link></div>}
    </Space>
   </Card>
   {usesAccounts && user.system_admin && <Card title={t('本地账号')}><BusinessAccounts key={db.id} db={db}/></Card>}
  </Space>
  <Modal title={t('API Key 已生成')} open={!!issuedKey} onCancel={()=>setIssuedKey('')} onOk={()=>setIssuedKey('')} okText={t('已保存，关闭')} cancelButtonProps={{style:{display:'none'}}} destroyOnHidden>
   <Space orientation="vertical" style={{width:'100%'}}>
    <Alert type="warning" showIcon title={t('请立即复制并保存，关闭后无法再次查看。')} />
    <Input.Password aria-label="API Key" value={issuedKey} readOnly autoComplete="off" />
    <Button onClick={()=>void navigator.clipboard.writeText(issuedKey).then(()=>message.success(t('API Key 已复制'))).catch(()=>message.error(t('复制失败，请手动复制')))}>{t('复制 API Key')}</Button>
   </Space>
  </Modal>
  <Modal {...saveModalProps} title={t('登录选项')} open={optionsOpen} onCancel={() => setOptionsOpen(false)} onOk={() => void saveOptions()} confirmLoading={busy} destroyOnHidden>
   {error && <Alert type="error" showIcon title={error} style={{ marginBottom: 16 }} />}
   <Form form={form} layout="vertical">
    <Form.Item name="label" label={t('登录页名称')}><Input maxLength={40} placeholder={db.name} /></Form.Item>
    {db.driver === 'sqlite' && <Form.Item name="employee_directory" label={t('校验演示库员工目录')} valuePropName="checked" tooltip={t('仅用于证券演示库，校验工号、员工编号和在职状态。')}><Switch /></Form.Item>}
   </Form>
  </Modal>
 </>;
}

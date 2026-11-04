import { useI18n } from './ui/preferences';
import { createContext, useContext, useEffect, useState, type ReactNode } from 'react';
import { Alert, Button, Card, Form, Input, Spin, Typography } from 'antd';
import { platformRequest } from './api';
export interface PlatformUser { id: number; username: string; name: string; active: boolean; system_admin: boolean }
const AuthContext = createContext<{ user: PlatformUser; logout: () => void } | null>(null);
export function useAuth() { return useContext(AuthContext)!; }
export function AuthBoundary({ children }: { children: ReactNode }) {
 const t=useI18n();
 const [method,setMethod]=useState<{mode:string;name:string;type:string}>();
 const [user, setUser] = useState<PlatformUser>(); const [loading, setLoading] = useState(true); const [busy, setBusy] = useState(false); const [error, setError] = useState<string>();
 useEffect(() => {void platformRequest<{mode:string;name:string;type:string}>('/api/auth/method').then(setMethod).catch(()=>undefined); void platformRequest<PlatformUser>('/api/auth/me').then(setUser).catch(() => undefined).finally(() => setLoading(false)); const expire=()=>{setUser(undefined);void platformRequest<{mode:string;name:string;type:string}>('/api/auth/method').then(setMethod).catch(()=>undefined)};window.addEventListener('platform-session-expired',expire);return()=>window.removeEventListener('platform-session-expired',expire); }, []);
 const login = async (values: { username: string; password: string }) => { setBusy(true);setError(undefined);try { setUser(await platformRequest<PlatformUser>('/api/auth/login',{method:'POST',body:JSON.stringify(values)})); } catch(e) {setError((e as Error).message);}finally{setBusy(false);} };
 const logout = () => { void platformRequest('/api/auth/logout',{method:'POST',body:'{}'}).then(()=>setUser(undefined)).catch(e=>setError(e.message)); };
 if(loading)return <div style={{padding:80,textAlign:'center'}}><Spin tip={t("正在检查登录状态")} /></div>;
 if(!user)return <main style={{maxWidth:420,margin:'12vh auto',padding:20}}><Card><Typography.Title level={3}>DataScope</Typography.Title><Typography.Paragraph type="secondary">{t("登录 DataScope 数据库管理与受控查询平台")}</Typography.Paragraph>{error&&<Alert type="error" title={error}/>}<Typography.Paragraph>{method?.name}</Typography.Paragraph>{method?.type==='ENTERPRISE_OAUTH'?<Button type="primary" block href="/api/auth/enterprise/start">使用企业身份登录</Button>:<Form layout="vertical" onFinish={login}><Form.Item name="username" label={t("用户名")} rules={[{required:true}]}><Input autoComplete="username" autoFocus/></Form.Item><Form.Item name="password" label={t("密码")} rules={[{required:true}]}><Input.Password autoComplete="current-password"/></Form.Item><Button type="primary" htmlType="submit" loading={busy} block>{t("登录")}</Button></Form>}</Card></main>;
 return <AuthContext.Provider value={{user,logout}}>{children}</AuthContext.Provider>;
}

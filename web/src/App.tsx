import { useEffect, useState } from 'react';
import { Alert, Button, Form, Input, Layout, Menu, Modal, Select, Space, Tag } from 'antd';
import { SettingOutlined } from '@ant-design/icons';
import { SystemSettings } from './components/SystemSettings';
import { usePreferences } from './ui/preferences';
import { DiscoveryPage } from './pages/Discovery';
import { DataBrowser } from './pages/DataBrowser';
import { McpManagement } from './pages/McpManagement';
import { DatabaseSettings } from './pages/DatabaseSettings';
import { databases, selectDatabase, selectedDatabase, type Database } from './api';
import { SqlWorkbench } from './pages/SqlWorkbench';
import { AuditPage } from './pages/Audit';
import { KnowledgePage } from './pages/PlatformPages';
import { useAuth } from './Auth';
import { ConnectionFields } from './components/ConnectionFields';

type Page = 'database' | 'tables' | 'knowledge' | 'sql' | 'table' | 'mcp' | 'audit';

interface Route { page:Page;table?:string;tab?:string }
function parseHash():Route{
 const [path,query]=window.location.hash.replace(/^#\/?/,'').split('?');
 const [page,table]=path.split('/');
 if(page==='discovery'||page==='policies')return {page:'tables'};
 if(page==='access')return {page:'database'};
 if(page==='audit')return {page:'audit'};
 if(page==='database')return new URLSearchParams(query).get('tab')==='audit'?{page:'audit'}:{page};
 if(page==='tables'||page==='knowledge'||page==='sql'||page==='mcp')return {page};
 if(page==='table'||table)return {page:'table',table:table?decodeURIComponent(table):undefined};
 return {page:'database'};
}

const titles:Record<Page,string>={database:'数据库配置',tables:'表配置与权限',knowledge:'业务知识',sql:'SQL 控制台',table:'表数据',audit:'操作日志',mcp:'MCP 配置'};

export function App() {
 const {user,logout}=useAuth();
  const [route, setRoute] = useState(parseHash);
  const {dark,t}=usePreferences();
 const [settingsOpen,setSettingsOpen]=useState(false);
 const [items,setItems]=useState<Database[]>([]);
 const [dbID,setDbID]=useState(selectedDatabase);
 const [adding,setAdding]=useState(false);
 const [creating,setCreating]=useState(false);
 const [testing,setTesting]=useState(false);
 const [testResult,setTestResult]=useState<string>();
 const [error,setError]=useState<string>();
 const [form]=Form.useForm();
 const refresh=()=>databases.list().then(setItems).catch(e=>setError(e.message));
 useEffect(()=>{void refresh();},[]);
 const switchDB=(id:string)=>{selectDatabase(id);setDbID(id);if(route.page==='table') window.location.hash='#/table';};
 useEffect(()=>{if(items.length && !items.some(d=>d.id===dbID)) switchDB(items[0].id);},[items,dbID]);
 const connectionValues=async()=>{
 const values=await form.validateFields();
 if(values.driver==='sqlite') return {name:values.name,driver:values.driver,path:values.path};
 const connection={...values.connection};if(values.driver==='mysql') connection.schema=connection.database;
 return {name:values.name,driver:values.driver,connection,password:values.password};
 };
 const testConnection=async()=>{
  try { const values=await connectionValues();setTesting(true);setError(undefined);setTestResult(undefined);const result=await databases.test(values);setTestResult(t("连接成功，发现 {0} 张表。", {"0":(result.table_count)})); }
  catch(e) {if(e instanceof Error)setError(e.message);}finally{setTesting(false);}
 };
 const add=async()=>{
  try { const values=await connectionValues();setCreating(true);const d=await databases.add(values);await refresh();switchDB(d.id);window.location.hash='#/tables';setAdding(false);form.resetFields();setTestResult(undefined);setError(undefined); }
  catch(e) { if(e instanceof Error) setError(e.message); } finally {setCreating(false);}
 };

  useEffect(() => {
    const onHash = () => {
      // Preserve table names and query filters in bookmarked legacy links.
      const hash = window.location.hash.replace(/^#\/?data(?=\/|\?|$)/, '#/table');
      if (hash !== window.location.hash) window.history.replaceState(null, '', hash);
      setRoute(parseHash());
    };
    onHash();
    window.addEventListener('hashchange', onHash);
    return () => window.removeEventListener('hashchange', onHash);
  }, []);


  const current=items.find(d=>d.id===dbID);
 const can=(p:string)=>user.system_admin||!!current?.permissions?.includes(p);
 const visiblePages=(Object.keys(titles) as Page[]).filter(p=>p==='audit'?(user.system_admin||items.some(d=>d.permissions?.includes('audit.read'))):p==='database'?(user.system_admin||can('config.read')||can('grant')):p==='tables'?can('config.read'):p==='knowledge'?can('knowledge.write'):p==='table'?can('raw_query'):p==='sql'?can('query'):can('mcp.write'));
 const activePage=!current&&user.system_admin&&route.page!=='audit'?'database':visiblePages.includes(route.page)?route.page:visiblePages[0];
 const openTable = (table: string, filter?: { col: string; val: string }) => {
    const qs = filter ? `?${new URLSearchParams({ col: filter.col, val: filter.val })}` : '';
    window.location.hash = `#/table/${encodeURIComponent(table)}${qs}`;
  };

  return (
    <>
      <Layout style={{ minHeight: '100vh', background: 'transparent' }}>
        <Layout.Header className="app-header">
          <Space><span className="app-title">DataScope</span><Select aria-label={t("切换数据库")} value={dbID} onChange={switchDB} style={{width:190}} options={items.map(d=>({value:d.id,label:d.name}))}/>{user.system_admin&&<Button onClick={()=>{setError(undefined);setTestResult(undefined);setAdding(true);}}>{t("＋ 数据库")}</Button>}</Space>
          <Menu
            mode="horizontal"
            theme={dark ? 'dark' : 'light'}
            selectedKeys={[activePage]}
            style={{ flex: 1, minWidth: 0, background: 'transparent', borderBottom: 'none' }}
            items={visiblePages.map((key) => ({ key, label: t(titles[key]) }))}
            onClick={({ key }) => (window.location.hash = `#/${key}`)}
          />
          <Space><Tag color="blue">{user.name||user.username}</Tag><Button onClick={logout}>{t('退出')}</Button><Button aria-label={t('系统设置')} title={t('系统设置')} icon={<SettingOutlined/>} onClick={()=>setSettingsOpen(true)}/></Space>
        </Layout.Header>
        <Layout.Content className="app-content" key={activePage==='audit'?'audit':dbID}>
 {activePage==='database'&&<DatabaseSettings db={current} onSaved={()=>void refresh()}/>}
 {activePage==='audit'&&<AuditPage databases={items}/>}
 {activePage==='knowledge'&&current&&<KnowledgePage databaseID={dbID}/>}
 {!activePage&&<Alert type="info" title={t("当前账号尚未获得数据库权限，请联系系统管理员。")}/>}
 {error && <Alert type="error" title={error} closable onClose={()=>setError(undefined)}/>}
          {activePage === 'table' && <DataBrowser table={route.table} onOpenTable={openTable} />}
          {activePage === 'sql' && <SqlWorkbench permissions={current?.permissions||[]} businessUserID={current?.business_user_id||0} />}
          {activePage === 'tables' && <DiscoveryPage id={dbID} onSaved={()=>{void refresh();}} />}
          {activePage === 'mcp' && <McpManagement id={dbID} onSaved={()=>{void refresh();}} />}
        </Layout.Content>
      </Layout>
      <Modal title={t("添加数据库")} centered styles={{body:{maxHeight:'calc(100dvh - 240px)',overflowY:'auto',paddingRight:8}}} open={adding} onCancel={()=>{setAdding(false);form.resetFields();}} onOk={add} confirmLoading={creating} okText={t("添加数据库")} footer={(_, {OkBtn,CancelBtn})=><Space><Button onClick={testConnection} loading={testing} disabled={creating}>{t("测试连接")}</Button><CancelBtn/><OkBtn/></Space>} destroyOnHidden>
       {error && <Alert type="error" title={error}/>}
       {testResult&&<Alert type="success" title={testResult}/>}
       <Form form={form} layout="vertical" initialValues={{driver:'sqlite'}} onValuesChange={()=>setTestResult(undefined)}><Form.Item name="name" label={t("显示名称")} rules={[{required:true}]}><Input placeholder={t("例如：业务数据库")}/></Form.Item><ConnectionFields form={form}/></Form>
       <p>{t("添加后自动侦查全部表、字段与注释，生成映射和结构快照。选择开放范围后，再开启 MCP。")}</p>
      </Modal>
      <SystemSettings open={settingsOpen} onClose={()=>setSettingsOpen(false)}/>
    </>
  );
}

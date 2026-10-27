import { useEffect, useState } from 'react';
import { Alert, Button, ConfigProvider, Form, Input, Layout, Menu, Modal, Select, Space, Tag, theme, type ThemeConfig } from 'antd';
import zhCN from 'antd/locale/zh_CN';
import { DiscoveryPage } from './pages/Discovery';
import { DataBrowser } from './pages/DataBrowser';
import { McpManagement } from './pages/McpManagement';
import { DatabaseManager } from './pages/DatabaseManager';
import { databases, selectDatabase, selectedDatabase, type Database } from './api';
import { SqlWorkbench } from './pages/SqlWorkbench';
import { AccessPage, AuditPage, KnowledgePage } from './pages/PlatformPages';
import { useAuth } from './Auth';
import { ConnectionFields } from './components/ConnectionFields';

type Page = 'discovery' | 'table' | 'sql' | 'policies' | 'mcp' | 'access' | 'audit' | 'knowledge';

interface Route {
  page: Page;
  table?: string;
}

function parseHash(): Route {
  const [path] = window.location.hash.replace(/^#\/?/, '').split('?');
  const [page, table] = path.split('/');
  if (page === 'discovery' || page === 'sql' || page === 'policies' || page === 'mcp' || page === 'access' || page === 'audit' || page === 'knowledge') return { page };
  return { page: page === 'table' || table ? 'table' : 'policies', table: table ? decodeURIComponent(table) : undefined };
}

function useDarkMode() {
  const query = window.matchMedia('(prefers-color-scheme: dark)');
  const [dark, setDark] = useState(query.matches);
  useEffect(() => {
    const listener = (e: MediaQueryListEvent) => setDark(e.matches);
    query.addEventListener('change', listener);
    return () => query.removeEventListener('change', listener);
  }, [query]);
  return dark;
}

// Apple-style frosted glass tokens; the translucent layers let the body gradient show through.
function buildTheme(dark: boolean): ThemeConfig {
  const glass = dark ? 'rgba(36, 36, 40, 0.55)' : 'rgba(255, 255, 255, 0.6)';
  const glassStrong = dark ? 'rgba(28, 28, 32, 0.72)' : 'rgba(255, 255, 255, 0.74)';
  return {
    algorithm: dark ? theme.darkAlgorithm : theme.defaultAlgorithm,
    token: {
      fontFamily: '-apple-system, BlinkMacSystemFont, "SF Pro Text", "PingFang SC", "Helvetica Neue", Arial, sans-serif',
      fontSize: 13,
      controlHeight: 30,
      controlHeightSM: 24,
      borderRadius: 8,
      borderRadiusLG: 12,
      colorPrimary: '#0071e3',
      colorBgContainer: glass,
      colorBgElevated: glassStrong,
      colorBgLayout: 'transparent',
      colorBorderSecondary: dark ? 'rgba(255, 255, 255, 0.08)' : 'rgba(255, 255, 255, 0.85)',
    },
    components: {
      Layout: { headerBg: glassStrong, headerHeight: 44 },
      Menu: { horizontalLineHeight: '42px' },
      Card: { headerHeight: 40, headerHeightSM: 36, paddingLG: 14 },
      Table: {
        colorBgContainer: 'transparent',
        headerBg: dark ? '#262629' : '#f5f5f7',
        // Set explicitly: antd derives these from the translucent container, which renders near-black on hover.
        rowHoverBg: dark ? '#2c2c30' : '#f0f6ff',
        bodySortBg: dark ? '#222225' : '#fafafc',
        headerSortHoverBg: dark ? '#303034' : '#ebebf0',
        cellPaddingBlock: 6,
        cellPaddingInline: 10,
        cellPaddingBlockSM: 4,
        cellPaddingInlineSM: 8,
      },
    },
  };
}

const titles: Record<Page, string> = {
 discovery:'数据库侦查',access:'用户与授权',audit:'日志审计',knowledge:'业务知识',
  sql: 'SQL控制台',
  table: '表数据',
  policies: '数据库访问策略',
  mcp: 'MCP 配置与测试',
};

export function App() {
 const {user,logout}=useAuth();
  const [route, setRoute] = useState(parseHash);
  const dark = useDarkMode();
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
 if(values.driver==='sqlite') return {id:values.id,name:values.name,driver:values.driver,path:values.path};
 const connection={...values.connection};if(values.driver==='mysql') connection.schema=connection.database;
 return {id:values.id,name:values.name,driver:values.driver,connection};
 };
 const testConnection=async()=>{
  try { const values=await connectionValues();setTesting(true);setError(undefined);setTestResult(undefined);const result=await databases.test(values);setTestResult(`连接成功，发现 ${result.table_count} 张表。`); }
  catch(e) {if(e instanceof Error)setError(e.message);}finally{setTesting(false);}
 };
 const add=async()=>{
  try { const values=await connectionValues();setCreating(true);const d=await databases.add(values);await refresh();switchDB(d.id);window.location.hash='#/discovery';setAdding(false);form.resetFields();setTestResult(undefined);setError(undefined); }
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

  useEffect(() => {
    document.documentElement.dataset.theme = dark ? 'dark' : 'light';
  }, [dark]);

  const current=items.find(d=>d.id===dbID);
 const can=(p:string)=>user.system_admin||!!current?.permissions?.includes(p);
 const visiblePages=(Object.keys(titles) as Page[]).filter(p=>p==='access'?(user.system_admin||can('grant')):p==='audit'?can('audit.read'):p==='knowledge'?can('knowledge.write'):p==='table'?can('raw_query'):p==='sql'?can('query'):p==='mcp'?can('mcp.write'):can('config.read'));
 const activePage=!current&&user.system_admin?'access':visiblePages.includes(route.page)?route.page:visiblePages[0];
 const openTable = (table: string, filter?: { col: string; val: string }) => {
    const qs = filter ? `?${new URLSearchParams({ col: filter.col, val: filter.val })}` : '';
    window.location.hash = `#/table/${encodeURIComponent(table)}${qs}`;
  };

  return (
    <ConfigProvider locale={zhCN} theme={buildTheme(dark)}>
      <Layout style={{ minHeight: '100vh', background: 'transparent' }}>
        <Layout.Header className="app-header">
          <Space><span className="app-title">DataScope</span><Select aria-label="切换数据库" value={dbID} onChange={switchDB} style={{width:190}} options={items.map(d=>({value:d.id,label:d.name}))}/>{user.system_admin&&<Button onClick={()=>{setError(undefined);setTestResult(undefined);setAdding(true);}}>＋ 数据库</Button>}</Space>
          <Space><Tag color="blue">{user.name||user.username}</Tag><Button onClick={logout}>退出</Button></Space>
          <Menu
            mode="horizontal"
            theme={dark ? 'dark' : 'light'}
            selectedKeys={[activePage]}
            style={{ flex: 1, minWidth: 0, background: 'transparent', borderBottom: 'none' }}
            items={visiblePages.map((key) => ({ key, label: titles[key] }))}
            onClick={({ key }) => (window.location.hash = `#/${key}`)}
          />
        </Layout.Header>
        <Layout.Content className="app-content" key={dbID}>
 {activePage==='access'&&<AccessPage db={current} onSaved={()=>void refresh()}/>}
 {activePage==='audit'&&current&&<AuditPage databaseID={dbID}/>}
 {activePage==='knowledge'&&current&&<KnowledgePage databaseID={dbID}/>}
 {!activePage&&<Alert type="info" title="当前账号尚未获得数据库权限，请联系系统管理员。"/>}
 {error && <Alert type="error" title={error} closable onClose={()=>setError(undefined)}/>}
          {activePage === 'table' && <DataBrowser table={route.table} onOpenTable={openTable} />}
          {activePage === 'sql' && <SqlWorkbench permissions={current?.permissions||[]} businessUserID={current?.business_user_id||0} />}
          {activePage === 'discovery' && <DiscoveryPage id={dbID} onSaved={()=>{void refresh();}} />}
          {activePage === 'policies' && <DatabaseManager id={dbID} onSaved={()=>{void refresh();}} />}
          {activePage === 'mcp' && <McpManagement id={dbID} onSaved={()=>{void refresh();}} />}
        </Layout.Content>
      </Layout>
      <Modal title="添加数据库" centered styles={{body:{maxHeight:'calc(100dvh - 240px)',overflowY:'auto',paddingRight:8}}} open={adding} onCancel={()=>setAdding(false)} onOk={add} confirmLoading={creating} okText="添加数据库" footer={(_, {OkBtn,CancelBtn})=><Space><Button onClick={testConnection} loading={testing} disabled={creating}>测试连接</Button><CancelBtn/><OkBtn/></Space>} destroyOnHidden>
       {error && <Alert type="error" title={error}/>}
       {testResult&&<Alert type="success" title={testResult}/>}
       <Form form={form} layout="vertical" initialValues={{driver:'sqlite'}} onValuesChange={()=>setTestResult(undefined)}><Form.Item name="name" label="显示名称" rules={[{required:true}]}><Input placeholder="例如：业务数据库"/></Form.Item><Form.Item name="id" label="数据库 ID" rules={[{required:true},{pattern:/^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$/,message:'使用字母、数字、下划线或连字符，最多 64 位'}]}><Input placeholder="business"/></Form.Item><ConnectionFields form={form}/></Form>
       <p>添加后自动侦查全部表、字段与注释，生成映射和结构快照。选择开放范围后，再开启 MCP。</p>
      </Modal>
    </ConfigProvider>
  );
}

import { useAutoHideScrollbars } from './autoHideScrollbars';
import { capabilityEnabled } from './components/CapabilitySwitch';
import { DatabaseDocumentation } from './components/DatabaseDocumentation';
import { lazy, Suspense, useEffect, useLayoutEffect, useState } from 'react';
import { Alert, Button, Layout, Menu, Select, Space, Spin, Tag, type MenuProps } from 'antd';
import { SettingOutlined, HistoryOutlined, DatabaseOutlined, FilterOutlined, CodeOutlined, BookOutlined, ConsoleSqlOutlined, TableOutlined, AuditOutlined, ApiOutlined, FileSearchOutlined, PlayCircleOutlined, LayoutOutlined, DesktopOutlined } from '@ant-design/icons';
import { UserGuide } from './components/UserGuide';
import { SystemSettings } from './components/SystemSettings';
import { usePreferences } from './ui/preferences';
import { QueryConfiguration } from './pages/QueryConfiguration';
import { Actions } from './pages/Actions';
import { DataBrowser } from './pages/DataBrowser';
import { McpManagement } from './pages/McpManagement';
import { DatabaseSettings } from './pages/DatabaseSettings';
import { databases, selectDatabase, selectedDatabase, type Database } from './api';
import { SqlWorkbench } from './pages/SqlWorkbench';
import { AuditPage } from './pages/Audit';
import { useAuth } from './Auth';
import { DatabaseIcon } from './components/DatabaseIcon';
import { AddDatabase } from './components/AddDatabase';
import { NavigationContext, navigate, readRoute, setRouteParams, startNavigation, type Page } from './navigation';

const DataSources = lazy(() => import('./pages/DataSources'));
const DataRunHistory = lazy(() => import('./pages/DataRunHistory'));
const ViewManagement = lazy(() => import('./pages/ViewManagement'));
const DDLManagement = lazy(() => import('./pages/DDLManagement'));
const ViewWorkbench = lazy(() => import('./pages/ViewWorkbench'));
const KnowledgeWorkbench = lazy(() => import('./pages/KnowledgeWorkbench'));
const ActionWorkbench = lazy(() => import('./pages/ActionWorkbench'));
const KnowledgePage = lazy(() => import('./pages/KnowledgeManagement').then(module => ({ default: module.KnowledgePage })));

const icons={'data-runs':<HistoryOutlined/>,sources:<DatabaseOutlined/>,ddl:<ConsoleSqlOutlined/>,views:<LayoutOutlined/>,database:<DatabaseOutlined/>,tables:<FilterOutlined/>,actions:<CodeOutlined/>,knowledge:<BookOutlined/>,'knowledge-workbench':<FileSearchOutlined/>,'action-workbench':<PlayCircleOutlined/>,'view-workbench':<DesktopOutlined/>,'sql-workbench':<ConsoleSqlOutlined/>,table:<TableOutlined/>,audit:<AuditOutlined/>,mcp:<ApiOutlined/>};
const titles:Record<Page,string>={'data-runs':'数据运行记录',sources:'数据来源',ddl:'DDL 管理',views:'视图配置',database:'数据库配置',tables:'查询配置',actions:'动作配置',knowledge:'知识配置','sql-workbench':'查询工作台','knowledge-workbench':'知识工作台','action-workbench':'动作工作台','view-workbench':'视图工作台',table:'表数据',audit:'操作日志',mcp:'MCP 配置'};
const navigationGroups: Page[][] = [
 ['database', 'ddl', 'sources', 'tables', 'actions', 'knowledge', 'views', 'mcp'],
 ['data-runs', 'sql-workbench', 'knowledge-workbench', 'action-workbench', 'view-workbench', 'table'],
 ['audit'],
];

export function App() {
 useAutoHideScrollbars();
 const {user,logout}=useAuth();
  const [route, setRoute] = useState(readRoute);
  const {dark,t}=usePreferences();
 const [settingsOpen,setSettingsOpen]=useState(false);
 const [items,setItems]=useState<Database[]>([]);
 const [dbID,setDbID]=useState(() => {
  const id = readRoute().params.get('database') || selectedDatabase;
  if(id!==selectedDatabase)selectDatabase(id); return id;
 });
 const [adding,setAdding]=useState(false);
 const [error,setError]=useState<string>();
 const refresh=()=>databases.list().then(setItems).catch(e=>setError(e.message));
 useEffect(()=>{void refresh();},[]);
 const switchDB=(id:string)=>navigate(route.page, undefined, {database:id});
 useEffect(()=>{if(items.length && !items.some(d=>d.id===dbID)) navigate(route.page,undefined,{database:items[0].id},true);},[items,dbID]);
  useLayoutEffect(() => startNavigation(() => {
   const next = readRoute();
   const id = next.params.get('database') || selectedDatabase;
   if(id!==selectedDatabase)selectDatabase(id); setDbID(id); setRoute(next);
   setRouteParams({database:id}, true);
  }), []);


  const current=items.find(d=>d.id===dbID);
 const can=(p:string)=>user.system_admin||!!current?.permissions?.includes(p);
 const visiblePages=(Object.keys(titles) as Page[]).filter(p=>p==='sources'||p==='data-runs'?can('config.read')&&can('source.write'):p==='ddl'?current?.driver==='sqlite'&&!!current.ddl_enabled&&can('config.read'):p==='audit'?(user.system_admin||items.some(d=>d.permissions?.includes('audit.read'))):p==='database'?(user.system_admin||can('config.read')||can('grant')):p==='tables'?can('config.read'):p==='actions'?can('config.read')&&can('action.write'):p==='views'||p==='view-workbench'?can('config.read')&&can('view.write'):p==='knowledge'?can('knowledge.write'):p==='knowledge-workbench'?can('knowledge.read'):p==='action-workbench'?can('action.write'):p==='table'?can('raw_query'):p==='sql-workbench'?can('query'):(can('mcp.write')||can('action.write')));
 const navigationItems: NonNullable<MenuProps['items']> = [];
 navigationGroups.forEach((group, index) => {
  const pages = group.filter(page => visiblePages.includes(page));
  if (!pages.length) return;
  if (navigationItems.length) navigationItems.push({ type: 'divider', key: `section-${index}` });
  navigationItems.push(...pages.map(key => ({ key, icon: icons[key], label: current && ['actions', 'knowledge', 'views'].includes(key) ? <span className="sidebar-capability-label"><span>{t(titles[key])}</span><span className="sidebar-capability-status">{t(capabilityEnabled(current,key as 'actions'|'knowledge'|'views') ? '已打开' : '打开')}</span></span> : t(titles[key]) })));
 });
 const activePage=!current&&user.system_admin&&route.page!=='audit'?'database':visiblePages.includes(route.page)?route.page:route.page==='ddl'&&visiblePages.includes('database')?'database':visiblePages[0];
 useEffect(()=>{if(current&&activePage&&activePage!==route.page)navigate(activePage,undefined,{database:dbID},true);},[current,activePage,route.page,dbID]);
 const openTable = (table: string, filter?: { col: string; val: string }) => {
    navigate('table', table, filter ? { col: filter.col, val: filter.val } : {});
  };

  return (
    <NavigationContext.Provider value={route}>
      <Layout className="app-shell" style={{ minHeight: '100vh', background: 'transparent' }}>
        <Layout.Header className="app-header">
          <Space><span className="app-title">DataScope</span><Select aria-label={t("切换数据库")} value={current?.id} onChange={switchDB} style={{width:190}} options={items.map(d=>({value:d.id,label:<Space size={6}><DatabaseIcon driver={d.driver}/>{d.name}</Space>}))}/>{current&&activePage!=='database'&&<DatabaseDocumentation key={current.id} db={current}/>}{user.system_admin&&<Button onClick={()=>{setError(undefined);setAdding(true);}}>{t("增加数据库")}</Button>}</Space>
          <Space><UserGuide/><Tag color="blue">{user.name||user.username}</Tag><Button onClick={logout}>{t('退出')}</Button><Button aria-label={t('系统设置')} title={t('系统设置')} icon={<SettingOutlined/>} onClick={()=>setSettingsOpen(true)}/></Space>
        </Layout.Header>
        <Layout className="workspace-layout" style={{background:'transparent'}}>
        <Layout.Sider className="workspace-sidebar" theme={dark?'dark':'light'} width={184} breakpoint="lg" collapsedWidth={56}>
         <div className="workspace-label">{current?.name||'数据库管理'}</div>
         <Menu mode="inline" theme={dark?'dark':'light'} selectedKeys={[activePage]} onClick={({key})=>navigate(key as Page)} items={navigationItems}/>
        </Layout.Sider>
        <Layout.Content className={`app-content${['tables', 'actions', 'knowledge', 'views'].includes(activePage || '') ? ' configuration-content' : ''}`} key={activePage==='audit'?'audit':dbID}>
 {activePage==='database'&&<DatabaseSettings key={current?.id} db={current} onSaved={()=>void refresh()} onRemoved={id=>{
  setItems(previous=>previous.filter(item=>item.id!==id));
  if(selectedDatabase===id){
   const next=items.find(item=>item.id!==id)?.id ?? '';
   selectDatabase(next);
   navigate('database',undefined,{database:next},true);
  }
 }}/>}
 {activePage==='sources'&&current&&<Suspense fallback={<Spin/>}><DataSources key={current.id} db={current}/></Suspense>}
 {activePage==='data-runs'&&current&&<Suspense fallback={<Spin/>}><DataRunHistory key={current.id} db={current}/></Suspense>}
 {activePage==='ddl'&&current&&<Suspense fallback={<Spin/>}><DDLManagement db={current} onSaved={()=>void refresh()}/></Suspense>}
 {activePage==='audit'&&<AuditPage databases={items}/>}
 {activePage==='views'&&current&&<Suspense fallback={<Spin/>}><ViewManagement db={current} onSaved={()=>void refresh()}/></Suspense>}
 {activePage==='view-workbench'&&current&&<Suspense fallback={<Spin/>}><ViewWorkbench db={current} onSaved={()=>void refresh()}/></Suspense>}
 {activePage==='knowledge'&&current&&<Suspense fallback={<Spin/>}><KnowledgePage db={current} onSaved={()=>void refresh()}/></Suspense>}
 {activePage==='knowledge-workbench'&&current&&<Suspense fallback={<Spin/>}><KnowledgeWorkbench databaseID={dbID}/></Suspense>}
 {activePage==='action-workbench'&&current&&<Suspense fallback={<Spin/>}><ActionWorkbench db={current}/></Suspense>}
 {!activePage&&<Alert type="info" title={t("当前账号尚未获得数据库权限，请联系系统管理员。")}/>}
 {error && <Alert type="error" title={error} closable onClose={()=>setError(undefined)}/>}
          {activePage === 'table' && <DataBrowser table={route.selection} onOpenTable={openTable} />}
          {activePage === 'sql-workbench' && <SqlWorkbench examples={current?.sql_examples} permissions={current?.permissions||[]} businessUserID={current?.business_user_id||0} />}
          {activePage === 'actions' && current && <Actions db={current} onSaved={()=>void refresh()}/> }
          {activePage === 'tables' && <QueryConfiguration id={dbID} onSaved={()=>{void refresh();}} />}
          {activePage === 'mcp' && <McpManagement id={dbID} onSaved={()=>{void refresh();}} />}
        </Layout.Content>
        </Layout>
      </Layout>
      {adding && <AddDatabase sources={items} onClose={()=>setAdding(false)} onAdded={db=>{setItems(previous=>previous.some(item=>item.id===db.id)?previous:[...previous,db]);navigate('tables',undefined,{database:db.id});setAdding(false);void refresh();}}/>}
      <SystemSettings open={settingsOpen} onClose={()=>setSettingsOpen(false)}/>
    </NavigationContext.Provider>
  );
}

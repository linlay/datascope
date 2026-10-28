import { useEffect, useState } from 'react';
import { Alert, Button, Card, Descriptions, Input, Space, Tabs, Typography, message } from 'antd';
import { managementCall, type Database } from '../api';
import { driverLabels } from '../components/ConnectionFields';
import { useAuth } from '../Auth';
import { AccessPage, AuditPage } from './PlatformPages';

export function DatabaseSettings({ db, tab, onSaved }: {db?:Database;tab?:string;onSaved:()=>void}) {
 const {user}=useAuth();
 const can=(p:string)=>user.system_admin||!!db?.permissions?.includes(p);
 const [name,setName]=useState(db?.name??'');
 const [busy,setBusy]=useState(false);
 const [error,setError]=useState<string>();
 useEffect(()=>{setName(db?.name??'');setError(undefined)},[db?.id,db?.name]);
 const run=async(work:()=>Promise<unknown>,text:string)=>{setBusy(true);setError(undefined);try{await work();message.success(text);onSaved()}catch(e){setError((e as Error).message)}finally{setBusy(false)}};
 const c=db?.connection;
 const items=[
  ...(db&&can('config.read')?[{key:'basic',label:'基本配置',children:<Card title="数据库基本配置" extra={<Space><Button loading={busy} onClick={()=>void run(()=>managementCall('test_connection',{database_id:db.id}),'数据库连接正常')}>测试连接</Button><Button type="primary" loading={busy} disabled={!can('config.write')||!name.trim()||name===db.name} onClick={()=>void run(()=>managementCall('update_database',{database_id:db.id,database:{id:db.id,revision:db.revision,name:name.trim()}}),'数据库名称已保存')}>保存</Button></Space>}>
   <Space orientation="vertical" size="middle" style={{width:'100%'}}>
    <div><Typography.Text>显示名称</Typography.Text><Input aria-label="数据库显示名称" style={{marginTop:8,maxWidth:480,display:'block'}} value={name} disabled={!can('config.write')} onChange={e=>setName(e.target.value)}/></div>
    <Descriptions column={{xs:1,md:2}} items={[
     {key:'type',label:'数据库类型',children:driverLabels[db.driver]||db.driver},
     ...(c?[
      {key:'address',label:'数据库地址',children:`${c.host}:${c.port}`},
      ...(c.database?[{key:'database',label:'数据库名称',children:c.database}]:[]),
      ...(c.service_name?[{key:'service',label:'服务名称',children:c.service_name}]:[]),
      {key:'schema',label:'Schema',children:c.schema},
      {key:'username',label:'连接账号',children:c.username},
      {key:'password',label:'连接密码',children:'已配置'},
     ]:[{key:'path',label:'SQLite 文件',children:db.path}]),
    ]}/>
   </Space>
  </Card>}]:[]),
  ...(can('grant')?[{key:'users',label:'用户配置',children:<AccessPage db={db} onSaved={onSaved}/>}]:[]),
  ...(db&&can('audit.read')?[{key:'audit',label:'操作日志',children:<AuditPage databaseID={db.id}/>}]:[]),
 ];
 const active=items.some(i=>i.key===tab)?tab:items[0]?.key;
 return <Space orientation="vertical" style={{width:'100%'}} size="middle">
  {!db&&<Alert type="info" title="先添加数据库，再配置表、字段与访问权限。"/>}
  {error&&<Alert type="error" showIcon title="操作失败" description={error} closable onClose={()=>setError(undefined)}/>}
  <Tabs activeKey={active} onChange={key=>{window.location.hash=`#/database?tab=${key}`}} items={items}/>
 </Space>;
}

import { useI18n } from '../ui/preferences';
import { useEffect, useState } from 'react';
import { Alert, Button, Card, Descriptions, Input, Space, Typography, message } from 'antd';
import { managementCall, type Database } from '../api';
import { driverLabels } from '../components/ConnectionFields';
import { useAuth } from '../Auth';
import { AccessPage } from './PlatformPages';

export function DatabaseSettings({ db, onSaved }: {db?:Database;onSaved:()=>void}) {
 const t=useI18n();
 const {user}=useAuth();
 const can=(p:string)=>user.system_admin||!!db?.permissions?.includes(p);
 const [name,setName]=useState(db?.name??'');
 const [busy,setBusy]=useState(false);
 const [error,setError]=useState<string>();
 useEffect(()=>{setName(db?.name??'');setError(undefined)},[db?.id,db?.name]);
 const run=async(work:()=>Promise<unknown>,text:string)=>{setBusy(true);setError(undefined);try{await work();message.success(text);onSaved()}catch(e){setError((e as Error).message)}finally{setBusy(false)}};
 const c=db?.connection;
 return <Space orientation="vertical" style={{width:'100%'}} size="middle">
  {!db&&<Alert type="info" title={t("先添加数据库，再配置表、字段与访问权限。")}/>}
  {error&&<Alert type="error" showIcon title={t("操作失败")} description={error} closable onClose={()=>setError(undefined)}/>}
  {db&&can('config.read')&&<Card title={t("基本信息")} extra={<Space><Button loading={busy} onClick={()=>void run(()=>managementCall('test_connection',{database_id:db.id}),t("数据库连接正常"))}>{t("测试连接")}</Button><Button type="primary" loading={busy} disabled={!can('config.write')||!name.trim()||name===db.name} onClick={()=>void run(()=>managementCall('update_database',{database_id:db.id,database:{id:db.id,revision:db.revision,name:name.trim()}}),t("数据库名称已保存"))}>{t("保存")}</Button></Space>}>
   <Space orientation="vertical" size="middle" style={{width:'100%'}}>
    <div><Typography.Text>{t("显示名称")}</Typography.Text><Input aria-label={t("数据库显示名称")} style={{marginTop:8,maxWidth:480,display:'block'}} value={name} disabled={!can('config.write')} onChange={e=>setName(e.target.value)}/></div>
    <Descriptions column={{xs:1,md:2}} items={[
     {key:'type',label:t("数据库类型"),children:t(driverLabels[db.driver]||db.driver)},
     ...(c?[
      {key:'address',label:t("数据库地址"),children:`${c.host}:${c.port}`},
      ...(c.database?[{key:'database',label:t("数据库名称"),children:c.database}]:[]),
      ...(c.service_name?[{key:'service',label:t("服务名称"),children:c.service_name}]:[]),
      {key:'schema',label:'Schema',children:c.schema},
      {key:'username',label:t("连接账号"),children:c.username},
      {key:'password',label:t("连接密码"),children:t("已配置")},
     ]:[{key:'path',label:t("SQLite 文件"),children:db.path}]),
    ]}/>
   </Space>
  </Card>}
  {can('grant')&&<AccessPage db={db} onSaved={onSaved}/>}
 </Space>;
}

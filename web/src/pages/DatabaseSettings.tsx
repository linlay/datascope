import { useI18n } from '../ui/preferences';
import { useEffect, useState } from 'react';
import { Alert, Button, Card, Descriptions, Input, Space, Typography, message, Switch, Tag } from 'antd';
import { managementCall, type Database, type DatabaseConnection } from '../api';
import { driverLabels } from '../components/ConnectionFields';
import { useAuth } from '../Auth';
import { AccessPage } from './PlatformPages';

export function DatabaseSettings({ db, onSaved }: {db?:Database;onSaved:()=>void}) {
 const t=useI18n();
 const {user}=useAuth();
 const can=(p:string)=>user.system_admin||!!db?.permissions?.includes(p);
 const [name,setName]=useState(db?.name??'');
 const [writeEnabled,setWriteEnabled]=useState(!!db?.write_enabled);
 const [writeUser,setWriteUser]=useState(db?.write_connection?.username||'');
 const [writePassword,setWritePassword]=useState('');
 const [writeEnv,setWriteEnv]=useState(db?.write_connection?.password_env||'');
 const [busy,setBusy]=useState(false);
 const [error,setError]=useState<string>();
 useEffect(()=>{setName(db?.name??'');setWriteEnabled(!!db?.write_enabled);setWriteUser(db?.write_connection?.username||'');setWriteEnv(db?.write_connection?.password_env||'');setWritePassword('');setError(undefined)},[db?.id,db?.name]);
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
  {db&&can('config.read')&&<Card title="管理存储与认证"><Descriptions column={1} items={[{key:'management',label:'业务管理库',children:<Typography.Text copyable>{db.management_path}</Typography.Text>},{key:'audit',label:'审计库',children:<Typography.Text copyable>{db.audit_path}</Typography.Text>},{key:'mode',label:'认证方式',children:<Space><Tag>{db.access?.policy.mode}</Tag>{db.access?.policy.name} · {db.access?.provider.name}</Space>}]}/><Typography.Paragraph type="secondary">认证方式由系统设置中的全局认证配置绑定；本库业务账号用于演示认证。审计写入失败不会阻止查询或 Action。</Typography.Paragraph></Card>}
  {db&&can('config.read')&&<Card title="Action 写入连接" extra={<Button type="primary" disabled={!can('action.write')} loading={busy} onClick={()=>void run(()=>{let write_connection:DatabaseConnection|undefined=db.write_connection;if(db.driver!=='sqlite'&&writeUser)write_connection={...db.connection!,username:writeUser,password_env:writePassword?undefined:writeEnv||undefined,credential_id:writePassword||writeEnv?undefined:db.write_connection?.credential_id};return managementCall('configure_actions',{database_id:db.id,database:{...db,write_enabled:writeEnabled,write_connection},write_password:writePassword})},'写入连接已保存')}>保存写入连接</Button>}><Space orientation="vertical" style={{width:'100%'}}><Space><Switch aria-label="启用 Action 写入" checked={writeEnabled} disabled={!can('action.write')} onChange={setWriteEnabled}/>启用 Action 写入</Space>{db.driver!=='sqlite'?<Space wrap><Input placeholder="独立写入账号" aria-label="独立写入账号" value={writeUser} onChange={e=>setWriteUser(e.target.value)}/><Input.Password placeholder="写入密码（留空保留）" value={writePassword} onChange={e=>setWritePassword(e.target.value)} autoComplete="new-password"/><Input placeholder="写入密码环境变量（可选）" value={writeEnv} onChange={e=>setWriteEnv(e.target.value)}/></Space>:<Typography.Text type="secondary">SQLite 使用同一业务文件的独立事务连接执行写入。</Typography.Text>}<Typography.Text type="secondary">Action 的参数、代码与试运行在“动作配置”中管理。</Typography.Text></Space></Card>}
  {can('grant')&&<AccessPage db={db} onSaved={onSaved}/>}
 </Space>;
}

import { useEffect,useState } from 'react';
import { Alert,Button,Card,Input,Select,Space,message } from 'antd';
import { databases,managementCall,type Database } from '../api';
import { useI18n } from '../ui/preferences';

export function SchemaScopeSettings({db,onSaved,disabled}:{db:Database;onSaved:()=>void;disabled:boolean}) {
 const t=useI18n();const c=db.connection!;
 const [schema,setSchema]=useState(c.schema||'');
 const [allowed,setAllowed]=useState<string[]>(c.allowed_schemas??(c.schema?[c.schema]:[]));
 const [available,setAvailable]=useState<string[]>([]);
 const [busy,setBusy]=useState(false);const [error,setError]=useState<string>();
 useEffect(()=>{setSchema(c.schema||'');setAllowed(c.allowed_schemas??(c.schema?[c.schema]:[]))},[c]);
 const run=async(work:()=>Promise<void>)=>{setBusy(true);setError(undefined);try{await work()}catch(e){setError((e as Error).message)}finally{setBusy(false)}};
 return <Card title={t('Schema 范围')}>
  <Space orientation="vertical" style={{width:'100%'}}>
   {error&&<Alert type="error" showIcon title={error}/>}
   <span>{t('默认 Schema（可选）')}</span><Input aria-label={t('默认 Schema（可选）')} value={schema} disabled={disabled||busy} onChange={e=>setSchema(e.target.value)} placeholder={t('留空时 SQL 必须使用 schema.table')}/>
   <span>{t('允许的 Schema 范围')}</span>
   <Select aria-label={t('允许的 Schema 范围')} mode="tags" style={{width:'100%'}} value={allowed} disabled={disabled||busy} onChange={setAllowed} options={[...new Set([...available,...allowed,...(schema?[schema]:[])])].map(value=>({value,label:value}))}/>
   <Alert type="info" showIcon title={t('Schema 范围只限定候选表。请到查询管理选择登记表并配置开放权限。移除 Schema 将移除其已登记表和查询策略。')}/>
   <Space>
    <Button loading={busy} onClick={()=>void run(async()=>setAvailable(await databases.schemas(db.id)))}>{t('发现可用 Schema')}</Button>
    <Button type="primary" loading={busy} disabled={disabled} onClick={()=>void run(async()=>{
     await managementCall('update_database',{database_id:db.id,database:{id:db.id,revision:db.revision,name:db.name,connection:{...c,schema:schema.trim(),...(db.driver==='mysql'?{database:schema.trim()}:{}),allowed_schemas:allowed}}});message.success(t('Schema 范围已保存'));onSaved();
    })}>{t('保存配置')}</Button>
   </Space>
  </Space>
 </Card>;
}

import { useI18n } from '../ui/preferences';
import { useEffect, useState } from 'react';
import { databases } from '../api';
import { Alert, Button, Col, Form, Input, InputNumber, Row, Select, type FormInstance } from 'antd';

export const driverLabels: Record<string, string> = { sqlite: 'SQLite', mysql: 'MySQL', postgresql: 'PostgreSQL', oracle: 'Oracle', dameng: '达梦 DM8' };
const defaultPorts: Record<string,number> = {mysql:3306,postgresql:5432,oracle:1521,dameng:5236};

export function ConnectionFields({ form }: { form: FormInstance }) {
 const t=useI18n();
 const driver = Form.useWatch('driver', form) || 'sqlite';
 const [urlError,setUrlError]=useState<string>();
 const [schemas,setSchemas]=useState<string[]>([]);
 const [probing,setProbing]=useState(false);
 const [probeResult,setProbeResult]=useState<string>();
 const allowedSchemas=Form.useWatch(['connection','allowed_schemas'],form)??[];
 const schemaValue=Form.useWatch(['connection','schema'],form);
 const databaseValue=Form.useWatch(['connection','database'],form);
 const defaultSchema=driver==='mysql'?databaseValue:schemaValue;
 const probe=async()=>{setProbing(true);setUrlError(undefined);try{const result=await databases.test(form.getFieldsValue(true));setSchemas(result.schemas);setProbeResult(result.empty?t('连接成功，但当前范围没有可读候选表。'):t('连接成功，可选择允许的 Schema。'));}catch(e){setUrlError((e as Error).message);}finally{setProbing(false)}};

 useEffect(()=>{
  if(driver==='sqlite')return;
  if(form.getFieldValue(['connection','port'])==null)form.setFieldValue(['connection','port'],defaultPorts[driver]);
  if((driver==='mysql'||driver==='postgresql')&&!form.getFieldValue(['connection','tls_mode']))form.setFieldValue(['connection','tls_mode'],'verify-full');
 },[driver,form]);
 return <>
  <Form.Item name="driver" label={t("数据库类型")} rules={[{ required: true }]}>
   <Select options={Object.entries(driverLabels).map(([value, label]) => ({ value, label:t(label) }))} onChange={value => {
    form.setFieldsValue({path:undefined,password:undefined,connection:{host:undefined,port:defaultPorts[value],database:undefined,service_name:undefined,schema:value==='postgresql'?'public':undefined,allowed_schemas:undefined,username:undefined,tls_mode:value==='mysql'||value==='postgresql'?'verify-full':undefined}});
   }}/>
  </Form.Item>
  {urlError&&<Alert type="error" showIcon title={urlError}/>}
  {driver === 'sqlite' ? <Form.Item name="path" preserve={false} label={t("SQLite 文件路径（服务端）")} rules={[{required:true}]}><Input placeholder="/absolute/path/database.sqlite3"/></Form.Item> : <>
   <Row gutter={12} wrap={false}>
    <Col flex="auto" style={{minWidth:0}}><Form.Item name={['connection','host']} preserve={false} label={t("数据库地址")} rules={[{required:true}]}><Input placeholder={t("IP 地址或域名")} autoComplete="off"/></Form.Item></Col>
    <Col flex="112px"><Form.Item name={['connection','port']} initialValue={defaultPorts[driver]} preserve={false} label={t("端口")} rules={[{required:true}]}><InputNumber min={1} max={65535} style={{width:'100%'}}/></Form.Item></Col>
   </Row>
   {(driver==='mysql'||driver==='postgresql')&&<Form.Item name={['connection','database']} preserve={false} label={t("数据库名称")} rules={driver==='postgresql'?[{required:true}]:[]}><Input placeholder={driver==='mysql'?t('默认数据库（可选）'):undefined}/></Form.Item>}
   {driver==='oracle'&&<Form.Item name={['connection','service_name']} preserve={false} label="Service name" rules={[{required:true}]}><Input placeholder={t("例如：FREEPDB1")}/></Form.Item>}
   {driver!=='mysql'&&<Form.Item name={['connection','schema']} preserve={false} label={t("默认 Schema（可选）")} tooltip={t("留空时，SQL 必须使用 schema.table；按数据库实际大小写填写。")}><Input placeholder={driver==='postgresql'?'public':t("例如：BUSINESS")}/></Form.Item>}
   <Form.Item name={['connection','allowed_schemas']} preserve={false} label={t('允许的 Schema 范围')} help={t('测试连接后选择，或手动填写。未指定范围时，有默认 Schema 则仅登记该 Schema；否则登记当前可发现的 Schema。')}><Select mode="tags" options={[...new Set([...schemas,...allowedSchemas,...(defaultSchema?[defaultSchema]:[])])].map(value=>({value,label:value}))} /></Form.Item>
   <Button loading={probing} onClick={()=>void probe()} style={{marginBottom:16}}>{t('测试连接并发现 Schema')}</Button>
   {probeResult&&<Alert type="info" showIcon title={probeResult} style={{marginBottom:16}}/>}
   <Form.Item name={['connection','username']} preserve={false} label={t("数据库只读账号")} rules={[{required:true}]}><Input autoComplete="off"/></Form.Item>
   <Form.Item name="password" preserve={false} label={t("数据库密码")} rules={[{required:true}]}><Input.Password autoComplete="new-password" placeholder={t("输入数据库密码")}/></Form.Item>
   {(driver==='mysql'||driver==='postgresql')&&<Form.Item name={['connection','tls_mode']} initialValue="verify-full" preserve={false} label={t("连接加密")} rules={[{required:true}]}><Select options={[{value:'verify-full',label:t("TLS：校验证书和主机名")},...(driver==='postgresql'?[{value:'require',label:t("TLS：只加密")}]:[]),{value:'disable',label:t("关闭 TLS（可信网络）")}]}/></Form.Item>}
  </>}
 </>;
}

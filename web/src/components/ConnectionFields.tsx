import { useI18n } from '../ui/preferences';
import { useEffect, useState } from 'react';
import { LinkOutlined } from '@ant-design/icons';
import { databases } from '../api';
import { DatabaseIcon } from './DatabaseIcon';
import { defaultPorts, isMySQLDriver, parseConnectionURL } from './connectionURL';
import { Alert, Button, Col, Form, Input, InputNumber, Row, Select, Space, type FormInstance } from 'antd';

export const driverLabels: Record<string, string> = { sqlite: 'SQLite', mysql: 'MySQL', oceanbase: 'OceanBase（MySQL 模式）', postgresql: 'PostgreSQL', oracle: 'Oracle', dameng: '达梦 DM8' };


export function ConnectionFields({ form, external = false, passwordRequired = true }: { form: FormInstance; external?: boolean; passwordRequired?: boolean }) {
 const t=useI18n();
 const driver = Form.useWatch('driver', form) || 'sqlite';
 const [urlOpen, setUrlOpen] = useState(false);
 const [urlValue, setUrlValue] = useState('');
 const [urlError, setUrlError] = useState<string>();
 const [parsed, setParsed] = useState(false);
 const [schemas,setSchemas]=useState<string[]>([]);
 const [probing,setProbing]=useState(false);
 const [probeResult,setProbeResult]=useState<string>();
 const allowedSchemas=Form.useWatch(['connection','allowed_schemas'],form)??[];
 const schemaValue=Form.useWatch(['connection','schema'],form);
 const databaseValue=Form.useWatch(['connection','database'],form);
 const defaultSchema=isMySQLDriver(driver)?databaseValue:schemaValue;
 const probe=async()=>{setProbing(true);setUrlError(undefined);try{const result=await databases.test(form.getFieldsValue(true));setSchemas(result.schemas);setProbeResult(result.empty?t('连接成功，但当前范围没有可读候选表。'):t('连接成功，可选择允许的 Schema。'));}catch(e){setUrlError((e as Error).message);}finally{setProbing(false)}};

 useEffect(()=>{
  if(driver==='sqlite')return;
  if(form.getFieldValue(['connection','port'])==null)form.setFieldValue(['connection','port'],defaultPorts[driver]);
  if((isMySQLDriver(driver)||driver==='postgresql')&&!form.getFieldValue(['connection','tls_mode']))form.setFieldValue(['connection','tls_mode'],'verify-full');
 },[driver,form]);
 const createSQLite = Form.useWatch('create_sqlite', form);
 const resetURL = () => { setUrlOpen(false); setUrlValue(''); setParsed(false); setUrlError(undefined); };
 const parseURL = (value: string) => {
  setUrlValue(value);
  setParsed(false); setUrlError(undefined);
  if (!value.trim()) return;
  try { const next = parseConnectionURL(value); if (next.driver !== driver) throw new Error(t('URL 与所选数据库类型不一致，请切换数据库类型或检查 URL。')); form.setFieldsValue({connection:undefined,password:undefined,path:undefined,create_sqlite:false,...next}); setParsed(true); }
  catch (e) { setUrlError((e as Error).message); }
 };
 return <>
  <Form.Item name="driver" label={t("数据库类型")} rules={[{ required: true }]}>
   <Select options={Object.entries(driverLabels).map(([value, label]) => ({ value, label:<Space size={6}><DatabaseIcon driver={value}/>{t(label)}</Space> }))} onChange={value => {
    resetURL(); setSchemas([]); setProbeResult(undefined);
    form.setFieldsValue({create_sqlite:false,path:undefined,password:undefined,connection:{host:undefined,port:defaultPorts[value],database:undefined,service_name:undefined,schema:value==='postgresql'?'public':undefined,allowed_schemas:undefined,username:undefined,tls_mode:isMySQLDriver(value)||value==='postgresql'?'verify-full':undefined}});
   }}/>
  </Form.Item>
  {!createSQLite && <div style={{ marginTop: -12, marginBottom: 16 }}>
   <Button type="link" size="small" icon={<LinkOutlined/>} style={{ paddingInline: 0 }} aria-expanded={urlOpen} aria-controls="database-connection-url" onClick={() => setUrlOpen(!urlOpen)}>{t(urlOpen ? '收起 URL 填写' : '从连接 URL 填写')}</Button>
  </div>}
  {urlOpen && !createSQLite && <div id="database-connection-url">
   <Form.Item label={t('连接 URL')} help={t('粘贴 URL 自动填入下方字段，仍可继续修改。支持常见 JDBC URL。')} validateStatus={urlError ? 'error' : undefined}>
    <Input.Password aria-label={t('连接 URL')} value={urlValue} placeholder={driver === 'sqlite' ? 'jdbc:sqlite:data/business.sqlite3' : driver === 'oracle' ? 'jdbc:oracle:thin:@//localhost:1521/FREEPDB1' : `${driver === 'dameng' ? 'dm' : driver}://user:password@localhost:${defaultPorts[driver]}/business`} autoComplete="off" onChange={event => parseURL(event.target.value)} />
   </Form.Item>
   {parsed && <Alert type="success" showIcon title={t('已解析连接 URL，请核对下方字段。')} style={{marginBottom:16}} />}
  </div>}
  {urlError && <Alert type="error" showIcon title={urlError} style={{marginBottom:16}} />}
  {driver === 'sqlite' ? <>
   {!external && <Form.Item name="create_sqlite" label={t('SQLite 来源')}><Select options={[{value:false,label:t('连接已有文件')},{value:true,label:t('新建项目数据库')}]} onChange={()=>{resetURL();form.setFieldsValue({path:undefined});}}/></Form.Item>}
   <Form.Item name="path" preserve={false} label={t(createSQLite ? '数据库文件名' : 'SQLite 文件路径（服务端）')} rules={[{required:true}, ...(createSQLite ? [{pattern:/^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}\.(db|sqlite|sqlite3)$/,message:t('请填写合法文件名，例如 business.sqlite3')}] : [])]}><Input placeholder={createSQLite ? 'business.sqlite3' : '/absolute/path/database.sqlite3'}/></Form.Item>
   {createSQLite && <Alert type="info" showIcon title={t('在服务端数据目录的 database 文件夹中新建，可在数据库配置中管理表结构。')} style={{marginBottom:16}}/>}
  </> : <>
   {driver==='oceanbase'&&<Alert type="info" showIcon title={t('仅支持 MySQL 租户；直连默认 2881，ODP 通常为 2883。账号填写 user@tenant 或 user@tenant#cluster。')} style={{marginBottom:16}}/>}
   <Row gutter={12} wrap={false}>
    <Col flex="auto" style={{minWidth:0}}><Form.Item name={['connection','host']} preserve={false} label={t("数据库地址")} rules={[{required:true}]}><Input placeholder={t("IP 地址或域名")} autoComplete="off"/></Form.Item></Col>
    <Col flex="112px"><Form.Item name={['connection','port']} initialValue={defaultPorts[driver]} preserve={false} label={t("端口")} rules={[{required:true}]}><InputNumber min={1} max={65535} style={{width:'100%'}}/></Form.Item></Col>
   </Row>
   {(isMySQLDriver(driver)||driver==='postgresql')&&<Form.Item name={['connection','database']} preserve={false} label={t("数据库名称")} rules={driver==='postgresql'?[{required:true}]:[]}><Input placeholder={isMySQLDriver(driver)?t('默认数据库（可选）'):undefined}/></Form.Item>}
   {driver==='oracle'&&<Form.Item name={['connection','service_name']} preserve={false} label="Service name" rules={[{required:true}]}><Input placeholder={t("例如：FREEPDB1")}/></Form.Item>}
   {!isMySQLDriver(driver)&&<Form.Item name={['connection','schema']} preserve={false} label={t("默认 Schema（可选）")} tooltip={t("留空时，SQL 必须使用 schema.table；按数据库实际大小写填写。")}><Input placeholder={driver==='postgresql'?'public':t("例如：BUSINESS")}/></Form.Item>}
   <Form.Item name={['connection','allowed_schemas']} preserve={false} label={t('允许的 Schema 范围')} help={t('测试连接后选择，或手动填写。未指定范围时，有默认 Schema 则仅登记该 Schema；否则登记当前可发现的 Schema。')}><Select mode="tags" options={[...new Set([...schemas,...allowedSchemas,...(defaultSchema?[defaultSchema]:[])])].map(value=>({value,label:value}))} /></Form.Item>
   {!external && <Button loading={probing} onClick={()=>void probe()} style={{marginBottom:16}}>{t('测试连接并发现 Schema')}</Button>}
   {probeResult&&<Alert type="info" showIcon title={probeResult} style={{marginBottom:16}}/>}
   <Form.Item name={['connection','username']} preserve={false} label={t("数据库只读账号")} rules={[{required:true}]}><Input autoComplete="off"/></Form.Item>
   <Form.Item name="password" preserve={false} label={t("数据库密码")} rules={[{required:passwordRequired}]}><Input.Password autoComplete="new-password" placeholder={t(passwordRequired ? "输入数据库密码" : "留空保留原密码")}/></Form.Item>
   {(isMySQLDriver(driver)||driver==='postgresql')&&<Form.Item name={['connection','tls_mode']} initialValue="verify-full" preserve={false} label={t("连接加密")} rules={[{required:true}]}><Select options={[{value:'verify-full',label:t("TLS：校验证书和主机名")},...(driver==='postgresql'?[{value:'require',label:t("TLS：只加密")}]:[]),{value:'disable',label:t("关闭 TLS（可信网络）")}]}/></Form.Item>}
  </>}
 </>;
}

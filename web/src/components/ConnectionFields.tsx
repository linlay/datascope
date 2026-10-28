import { useI18n } from '../ui/preferences';
import { useEffect } from 'react';
import { Col, Form, Input, InputNumber, Row, Select, type FormInstance } from 'antd';

export const driverLabels: Record<string, string> = { sqlite: 'SQLite', mysql: 'MySQL', postgresql: 'PostgreSQL', oracle: 'Oracle', dameng: '达梦 DM8' };
const defaultPorts: Record<string,number> = {mysql:3306,postgresql:5432,oracle:1521,dameng:5236};

export function ConnectionFields({ form }: { form: FormInstance }) {
 const t=useI18n();
 const driver = Form.useWatch('driver', form) || 'sqlite';
 useEffect(()=>{
  if(driver!=='sqlite')form.setFieldsValue({connection:{port:defaultPorts[driver],schema:driver==='postgresql'?'public':undefined,tls_mode:driver==='mysql'||driver==='postgresql'?'verify-full':undefined}});
 },[driver,form]);
 return <>
  <Form.Item name="driver" label={t("数据库类型")} rules={[{ required: true }]}>
   <Select options={Object.entries(driverLabels).map(([value, label]) => ({ value, label:t(label) }))} onChange={value => {
    form.setFieldsValue({path:undefined,password:undefined,connection:{host:undefined,port:defaultPorts[value],database:undefined,service_name:undefined,schema:value==='postgresql'?'public':undefined,username:undefined,tls_mode:value==='mysql'||value==='postgresql'?'verify-full':undefined}});
   }}/>
  </Form.Item>
  {driver === 'sqlite' ? <Form.Item name="path" preserve={false} label={t("SQLite 文件路径（服务端）")} rules={[{required:true}]}><Input placeholder="/absolute/path/database.sqlite3"/></Form.Item> : <>
   <Row gutter={12} wrap={false}>
    <Col flex="auto" style={{minWidth:0}}><Form.Item name={['connection','host']} preserve={false} label={t("数据库地址")} rules={[{required:true}]}><Input placeholder={t("IP 地址或域名")} autoComplete="off"/></Form.Item></Col>
    <Col flex="112px"><Form.Item name={['connection','port']} preserve={false} label={t("端口")} rules={[{required:true}]}><InputNumber min={1} max={65535} style={{width:'100%'}}/></Form.Item></Col>
   </Row>
   {(driver==='mysql'||driver==='postgresql')&&<Form.Item name={['connection','database']} preserve={false} label={t("数据库名称")} rules={[{required:true}]}><Input/></Form.Item>}
   {driver==='oracle'&&<Form.Item name={['connection','service_name']} preserve={false} label="Service name" rules={[{required:true}]}><Input placeholder={t("例如：FREEPDB1")}/></Form.Item>}
   {driver!=='mysql'&&<Form.Item name={['connection','schema']} preserve={false} label="Schema" rules={[{required:true}]} tooltip={t("按数据库中的实际大小写填写；每个数据源仅开放一个 schema。")}><Input placeholder={driver==='postgresql'?'public':t("例如：BUSINESS")}/></Form.Item>}
   <Form.Item name={['connection','username']} preserve={false} label={t("数据库只读账号")} rules={[{required:true}]}><Input autoComplete="off"/></Form.Item>
   <Form.Item name="password" preserve={false} label={t("数据库密码")} rules={[{required:true}]}><Input.Password autoComplete="new-password" placeholder={t("输入数据库密码")}/></Form.Item>
   {(driver==='mysql'||driver==='postgresql')&&<Form.Item name={['connection','tls_mode']} preserve={false} label={t("连接加密")} rules={[{required:true}]}><Select options={[{value:'verify-full',label:t("TLS：校验证书和主机名")},...(driver==='postgresql'?[{value:'require',label:t("TLS：只加密")}]:[]),{value:'disable',label:t("关闭 TLS（可信网络）")}]}/></Form.Item>}
  </>}
 </>;
}

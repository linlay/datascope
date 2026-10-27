import { Form, Input, InputNumber, Select, Typography, type FormInstance } from 'antd';

export const driverLabels: Record<string, string> = { sqlite: 'SQLite', mysql: 'MySQL', postgresql: 'PostgreSQL', oracle: 'Oracle', dameng: '达梦 DM8' };

export function ConnectionFields({ form }: { form: FormInstance }) {
 const driver = Form.useWatch('driver', form) || 'sqlite';
 return <>
  <Form.Item name="driver" label="数据库类型" rules={[{ required: true }]}>
   <Select options={Object.entries(driverLabels).map(([value, label]) => ({ value, label }))} onChange={value => {
    form.setFieldsValue({ path: undefined, connection: { host: undefined, port: ({mysql:3306,postgresql:5432,oracle:1521,dameng:5236} as Record<string,number>)[value], database: undefined, service_name: undefined, schema: value==='postgresql'?'public':undefined, username: undefined, password_env: undefined, tls_mode: value==='mysql'||value==='postgresql'?'verify-full':undefined } });
   }}/>
  </Form.Item>
  {driver === 'sqlite' ? <Form.Item name="path" preserve={false} label="SQLite 文件路径（服务端）" rules={[{required:true}]}><Input placeholder="/absolute/path/database.sqlite3"/></Form.Item> : <>
   <Form.Item name={['connection','host']} preserve={false} label="数据库地址" rules={[{required:true}]}><Input placeholder="例如：10.0.0.20"/></Form.Item>
   <Form.Item name={['connection','port']} preserve={false} label="端口" rules={[{required:true}]}><InputNumber min={1} max={65535} style={{width:'100%'}}/></Form.Item>
   {(driver==='mysql'||driver==='postgresql')&&<Form.Item name={['connection','database']} preserve={false} label="数据库名称" rules={[{required:true}]}><Input/></Form.Item>}
   {driver==='oracle'&&<Form.Item name={['connection','service_name']} preserve={false} label="Service name" rules={[{required:true}]}><Input placeholder="例如：FREEPDB1"/></Form.Item>}
   {driver!=='mysql'&&<Form.Item name={['connection','schema']} preserve={false} label="Schema" rules={[{required:true}]} tooltip="按数据库中的实际大小写填写；每个数据源仅开放一个 schema。"><Input placeholder={driver==='postgresql'?'public':'例如：BUSINESS'}/></Form.Item>}
   <Form.Item name={['connection','username']} preserve={false} label="数据库只读账号" rules={[{required:true}]}><Input autoComplete="off"/></Form.Item>
   <Form.Item name={['connection','password_env']} preserve={false} label="密码环境变量" rules={[{required:true},{pattern:/^DATASCOPE_DB_[A-Z0-9_]+$/,message:'使用 DATASCOPE_DB_ 开头的大写环境变量名称'}]} extra="在 API 和 MCP 服务端配置同名环境变量，值为数据库密码。此处只填写变量名称。"><Input placeholder="DATASCOPE_DB_BUSINESS_PASSWORD" autoComplete="off"/></Form.Item>
   {(driver==='mysql'||driver==='postgresql')&&<Form.Item name={['connection','tls_mode']} preserve={false} label="连接加密" rules={[{required:true}]}><Select options={[{value:'verify-full',label:'TLS：校验证书和主机名'},...(driver==='postgresql'?[{value:'require',label:'TLS：只加密'}]:[]),{value:'disable',label:'关闭 TLS（可信网络）'}]}/></Form.Item>}
   <Typography.Paragraph type="secondary">仅登记当前 schema 的数据表。查询使用受限 SELECT 语法，支持的语法由服务端校验。</Typography.Paragraph>
  </>}
 </>;
}

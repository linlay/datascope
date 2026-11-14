import { useEffect, useState } from 'react';
import { Alert, Button, Form, Input, Modal, Select, Space } from 'antd';
import { databases, type Database } from '../api';
import { useI18n } from '../ui/preferences';
import { ConnectionFields } from './ConnectionFields';
import { isMySQLDriver } from './connectionURL';
import { DatabaseClone } from './DatabaseClone';

export function AddDatabase({ sources, onClose, onAdded }: { sources: Database[]; onClose: () => void; onAdded: (db: Database) => void }) {
 const t = useI18n();
 const [form] = Form.useForm();
 const [busy, setBusy] = useState(false);
 const [testing, setTesting] = useState(false);
 const [error, setError] = useState<string>();
 const [testResult, setTestResult] = useState<string>();
 const [created, setCreated] = useState<Database>();
 const [cloneSource, setCloneSource] = useState<Database>();
 const mode = Form.useWatch('mode', form) || 'new';
 const upload = Form.useWatch('sourceMode', form) === 'upload';
 const createSQLite = Form.useWatch('create_sqlite', form);
 const sourceID = Form.useWatch('source_database_id', form);
 const pending = busy || testing;
 const valuesForConnection = (values: Record<string, any>) => {
  if (values.driver === 'sqlite') return { name: values.name.trim(), driver: values.driver, path: values.path, create_sqlite: !!values.create_sqlite };
  const connection = { ...values.connection };
  if (isMySQLDriver(values.driver)) connection.schema = connection.database;
  return { name: values.name.trim(), driver: values.driver, connection, password: values.password };
 };
 const selectSource = (id: string) => {
  const source = sources.find(db => db.id === id);
  if (!source) return;
  const connection = source.connection && {
   host: source.connection.host, port: source.connection.port, database: source.connection.database,
   service_name: source.connection.service_name, schema: source.connection.schema,
   allowed_schemas: source.connection.allowed_schemas, username: source.connection.username, tls_mode: source.connection.tls_mode,
  };
  form.setFieldsValue({ name: `${source.name} ${t('副本')}`, driver: source.driver, connection, path: undefined, password: undefined, create_sqlite: false, sourceMode: 'connection' });
  setError(undefined); setTestResult(undefined);
 };
 useEffect(() => {
  if (mode === 'clone' && sourceID) selectSource(sourceID);
 }, [mode, sourceID]);
 const test = async () => {
  try {
   const values = await form.validateFields();
   setTesting(true); setError(undefined); setTestResult(undefined);
   const result = await databases.test(valuesForConnection(values));
   setTestResult(t('连接成功，发现 {0} 张表。', { '0': result.table_count }));
  } catch (e) { if (e instanceof Error) setError(e.message); }
  finally { setTesting(false); }
 };
 const add = async () => {
  try {
   const values = await form.validateFields();
   setBusy(true); setError(undefined);
   const source = mode === 'clone' ? sources.find(db => db.id === values.source_database_id) : undefined;
   const db = upload ? await databases.importFile(values.name.trim(), values.file) : await databases.add(valuesForConnection(values));
   if (source) { setCloneSource(source); setCreated(db); }
   else onAdded(db);
  } catch (e) { if (e instanceof Error) setError(e.message); }
  finally { setBusy(false); }
 };
 return <Modal title={t('增加数据库')} open centered width={720} destroyOnHidden
  styles={{ body: { maxHeight: 'calc(100dvh - 240px)', overflowY: 'auto', paddingRight: 8 } }}
  closable={!pending} maskClosable={!pending} onCancel={() => { if (!pending) { if (created) onAdded(created); else onClose(); } }}
  footer={created ? <Button disabled={pending} onClick={() => onAdded(created)}>{t('保留新库，稍后配置')}</Button> : <Space>
   <Button onClick={() => void test()} loading={testing} disabled={busy || upload || createSQLite || (mode === 'clone' && !sourceID)}>{t('测试连接')}</Button>
   <Button disabled={pending} onClick={onClose}>{t('取消')}</Button>
   <Button type="primary" loading={busy} disabled={testing} onClick={() => void add()}>{t(mode === 'clone' ? '创建新库并预览克隆' : '增加数据库')}</Button>
  </Space>}>
  {created && cloneSource ? <Space orientation="vertical" style={{ width: '100%' }}>
   <Alert type="success" showIcon title={t('新数据库已增加，请核对并完成配置克隆。失败可重试，不会重复创建数据库。')} />
   <DatabaseClone db={created} sourceDatabase={cloneSource} onSaved={() => onAdded(created)} onBusyChange={setBusy} />
  </Space> : <>
   {error && <Alert type="error" showIcon title={error} />}
   {testResult && <Alert type="success" showIcon title={testResult} />}
   <Form className="configuration-form" form={form} layout="horizontal" labelAlign="left" labelWrap initialValues={{ mode: 'new', driver: 'sqlite', create_sqlite: false, sourceMode: 'connection' }} disabled={pending} onValuesChange={() => { setTestResult(undefined); setError(undefined); }}>
    <Form.Item name="mode" label={<span className="configuration-label-text">{t('增加方式')}</span>}><Select options={[{ value: 'new', label: t('全新配置') }, { value: 'clone', label: t('从已有数据库克隆') }]} onChange={() => { form.resetFields(['source_database_id', 'name', 'driver', 'connection', 'path', 'password', 'create_sqlite', 'sourceMode']); }} /></Form.Item>
    {mode === 'clone' && <>
     <Form.Item name="source_database_id" label={<span className="configuration-label-text">{t('来源库')}</span>} rules={[{ required: true, message: t('选择来源库') }]}><Select showSearch optionFilterProp="label" placeholder={t('选择来源库')} options={sources.map(db => ({ value: db.id, label: `${db.name} (${db.id})` }))} /></Form.Item>
     <Alert type="info" showIcon title={t('将所选配置复制到新数据库，来源库保持不变。不复制业务数据，目标库需要已有兼容的表结构。')} description={t('已填入来源连接供修改；请核对新库连接并重新输入密码。SQLite 请填写目标文件路径。')} style={{ marginBottom: 16 }} />
    </>}
    <Form.Item name="name" label={<span className="configuration-label-text">{t('显示名称')}</span>} rules={[{ required: true, whitespace: true }]}><Input placeholder={t('例如：业务数据库')} /></Form.Item>
    <Form.Item name="sourceMode" label={<span className="configuration-label-text">{t('创建方式')}</span>}><Select options={[{ value: 'connection', label: t('配置数据库连接') }, { value: 'upload', label: t('上传文件') }]} /></Form.Item>
    {upload ? <Form.Item name="file" label={<span className="configuration-label-text">{t('CSV 或 SQLite 文件')}</span>} preserve={false}
     extra={t('最大 20 MiB。CSV 使用 UTF-8 编码，首行为列名，数据按文本导入 data 表。')}
     getValueProps={() => ({})} getValueFromEvent={event => event.target.files?.[0]}
     rules={[{ required: true, message: t('请选择文件') }, { validator: async (_, file?: File) => {
      if (file && !/\.(csv|db|sqlite|sqlite3)$/i.test(file.name)) throw new Error(t('请选择 CSV 或 SQLite 文件'));
      if (file && file.size > 20 * 1024 * 1024) throw new Error(t('文件不能超过 20 MiB'));
     } }]}><input type="file" accept=".csv,.db,.sqlite,.sqlite3" disabled={pending} /></Form.Item> : <ConnectionFields form={form} preserveFields />}
   </Form>
   <p>{t('网络数据源先登记 Schema 范围，再到查询配置选择表并配置开放权限；SQLite 自动发现表结构。')}</p>
  </>}
 </Modal>;
}

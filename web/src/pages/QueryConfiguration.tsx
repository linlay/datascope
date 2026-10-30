import { useEffect, useState } from 'react';
import { Alert, Button, Form, Input, Modal, Space, Spin, Table, message } from 'antd';
import { databases, managementCall, type Database, type BusinessTool } from '../api';
import { DiscoveryPage } from './Discovery';
import { CodeEditor } from '../components/CodeEditor';
import { HelpTip } from '../components/HelpTip';
import { ParameterEditor, parameterMap, parameterRows, type ParameterRow } from '../components/ParameterEditor';
import { useI18n } from '../ui/preferences';
import { FileSearchOutlined } from '@ant-design/icons';
import { IconButton } from '../components/IconButton';

export function QueryConfiguration({ id, onSaved }: { id: string; onSaved: () => void }) {
 const t = useI18n();
 const [templatesOpen, setTemplatesOpen] = useState(false);
 const [templateDirty, setTemplateDirty] = useState(false);
 const [refreshVersion, setRefreshVersion] = useState(0);
 useEffect(() => { setTemplatesOpen(false); setTemplateDirty(false); }, [id]);
 const close = () => {
  if (templateDirty) {
   Modal.confirm({ title: t('放弃未保存的修改？'), onOk: () => setTemplatesOpen(false) });
  } else setTemplatesOpen(false);
 };
 return <>
  <DiscoveryPage key={`${id}:${refreshVersion}`} id={id} onSaved={onSaved} extraActions={({ busy, dirty }) => <IconButton label={t('查询模板')} help={dirty ? t('请先保存当前修改') : undefined} icon={<FileSearchOutlined/>} disabled={busy || dirty} onClick={() => setTemplatesOpen(true)}/>} />
  <Modal title={<Space>{t('查询模板')}<HelpTip label={t('查询模板')}>{t('可选：将常用 SELECT 保存成独立的 MCP 查询工具。例如 find_customer 按客户编号查询。参数通过 :arg_参数名 绑定，查询仍遵守权限规则。')}</HelpTip></Space>} open={templatesOpen} onCancel={close} width={1100} footer={null} styles={{ body: { maxHeight: '76vh', overflowY: 'auto' } }} destroyOnHidden>
   {templatesOpen && <QueryTemplates id={id} onSaved={() => { setRefreshVersion(version => version + 1); onSaved(); }} onDirty={setTemplateDirty} />}
  </Modal>
 </>;
}

function QueryTemplates({ id, onSaved, onDirty }: { id: string; onSaved: () => void; onDirty: (dirty: boolean) => void }) {
 const t = useI18n();
 const [db, setDB] = useState<Database>();
 const [items, setItems] = useState<BusinessTool[]>([]);
 const [editing, setEditing] = useState<number>();
 const [params, setParams] = useState<ParameterRow[]>([]);
 const [search, setSearch] = useState('');
 const [busy, setBusy] = useState(false);
 const [error, setError] = useState('');
 const [form] = Form.useForm();
 const dirty = JSON.stringify(items) !== JSON.stringify(db?.business_tools || []);
 useEffect(() => { onDirty(dirty); }, [dirty, onDirty]);
 useEffect(() => {
  let live = true;
  databases.get(id).then(next => { if (live) { setDB(next); setItems(next.business_tools || []); } }).catch(e => { if (live) setError(e.message); });
  return () => { live = false; };
 }, [id]);
 const edit = (index: number) => {
  const item = items[index] || { name: '', description: '', sql: 'SELECT ', parameters: {} };
  form.setFieldsValue(item); setParams(parameterRows(item.parameters)); setEditing(index); setError('');
 };
 const apply = async () => {
  try {
   const values = await form.validateFields(); const parameters = parameterMap(params); const next = [...items];
   next[editing!] = { ...values, parameters };
   if (next.some((item, index) => item.name === values.name && index !== editing)) throw new Error(t('查询名称重复'));
   setItems(next); setEditing(undefined); setError('');
  } catch (e) { setError((e as Error).message); }
 };
 const save = async () => {
  if (!db) return;
  setBusy(true); setError('');
  try {
   const next = await managementCall<Database>('configure_query_tools', { database_id: id, database: { ...db, business_tools: items } });
   setDB(next); setItems(next.business_tools || []); onSaved(); message.success(t('查询模板已保存'));
  } catch (e) { setError((e as Error).message); } finally { setBusy(false); }
 };
 const canWrite = db?.permissions?.includes('policy.write');
 if (!db && !error) return <Spin />;
 return <Space orientation="vertical" style={{ width: '100%' }} size="middle">
  {error && editing === undefined && <Alert type="error" title={error} />}
  <div className="query-toolbar">
   <Input.Search aria-label={t('搜索查询模板')} placeholder={t('搜索名称或说明')} allowClear value={search} onChange={e => setSearch(e.target.value)} style={{ width: 280, maxWidth: '100%' }} />
   <Space><Button disabled={!canWrite} onClick={() => edit(items.length)}>{t('新增模板')}</Button><Button type="primary" disabled={!canWrite || !dirty} loading={busy} onClick={() => void save()}>{t('保存')}</Button></Space>
  </div>
  <Table<BusinessTool> rowKey="name" size="small" locale={{ emptyText: t('暂无查询模板') }} dataSource={items.filter(item => (item.name + ' ' + item.description).toLowerCase().includes(search.toLowerCase()))} columns={[
   { title: t('名称'), dataIndex: 'name' }, { title: t('说明'), dataIndex: 'description' },
   { title: t('参数'), render: (_, item) => Object.keys(item.parameters || {}).join(', ') },
   { title: t('操作'), render: (_, item) => <Space><Button onClick={() => edit(items.indexOf(item))}>{canWrite ? t('编辑') : t('查看')}</Button><Button danger disabled={!canWrite} onClick={() => Modal.confirm({ title: t('删除查询模板？'), onOk: () => setItems(items.filter(other => item !== other)) })}>{t('删除')}</Button></Space> },
  ]} />
  <Modal title={t('编辑查询模板')} open={editing !== undefined} width={960} onCancel={() => setEditing(undefined)} onOk={() => void apply()} okButtonProps={{ disabled: !canWrite }} styles={{ body: { maxHeight: '72vh', overflowY: 'auto' } }}>
   {error && <Alert type="error" title={error} />}
   <Form layout="vertical" form={form} disabled={!canWrite}>
    <Form.Item label={t('名称')} name="name" rules={[{ required: true, pattern: /^[a-zA-Z][a-zA-Z0-9_]{0,63}$/ }]}><Input /></Form.Item>
    <Form.Item label={t('说明')} name="description" rules={[{ required: true }]}><Input /></Form.Item>
    <Form.Item label={t('查询 SQL')} name="sql" rules={[{ required: true }]} tooltip={t('业务参数使用 :arg_参数名；员工身份由服务端提供。')}><CodeEditor language="sql" readOnly={!canWrite} /></Form.Item>
    <Form.Item label={t('参数')}><ParameterEditor value={params} onChange={setParams} readOnly={!canWrite} constraints={false} optional={false} /></Form.Item>
   </Form>
  </Modal>
 </Space>;
}

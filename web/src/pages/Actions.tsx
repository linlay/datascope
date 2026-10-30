import { useEffect, useState } from 'react';
import { Alert, Button, Card, Form, Input, InputNumber, Modal, Select, Space, Switch, Table, Typography, message } from 'antd';
import { managementCall, type Database, type ActionDefinition } from '../api';
import { CodeEditor } from '../components/CodeEditor';
import { ParameterEditor, parameterMap, parameterRows, type ParameterRow } from '../components/ParameterEditor';
import { useI18n } from '../ui/preferences';

const example: ActionDefinition = {
 name: 'update_record', title: '更新记录', description: '更新当前用户可见的记录', confirm: true,
 parameters: { id: { type: 'integer', description: '记录编号' }, value: { type: 'string', description: '新值', max_length: 200 } },
 script: 'function run(args, ctx) {\n  const row = my.one`SELECT id FROM records WHERE id = ${args.id}`;\n  if (!row) deny("记录不存在或无权查看");\n  const n = db.exec`UPDATE records SET value = ${args.value} WHERE id = ${args.id}`;\n  if (n !== 1) deny("记录已变化，请重试");\n  return { id: args.id, updated: n };\n}',
};
export function Actions({ db, onSaved }: { db: Database; onSaved: () => void }) {
 const t = useI18n();
 const canWrite = db.permissions?.includes('action.write');
 const [items, setItems] = useState<ActionDefinition[]>(db.actions || []);
 const [editing, setEditing] = useState<number>();
 const [busy, setBusy] = useState(false);
 const [error, setError] = useState('');
 const [parameters, setParameters] = useState<ParameterRow[]>([]);
 const [search,setSearch]=useState('');
 const [trialName, setTrialName] = useState<string>();
 const [identity, setIdentity] = useState<number>(db.business_user_id || 1);
 const [args, setArgs] = useState('{}');
 const [result, setResult] = useState<unknown>();
 const [form] = Form.useForm();
 useEffect(() => { setItems(db.actions || []); setResult(undefined); setTrialName(undefined); setEditing(undefined); setError(''); }, [db.id, db.revision]);
 const edit = (i: number) => { const a = items[i] || { ...example, name: '', title: '', description: '' }; form.setFieldsValue(a); setParameters(parameterRows(a.parameters)); setEditing(i); setError(''); };
 const apply = async () => { try { const v = await form.validateFields(); const p = parameterMap(parameters); if(items.some((a,i)=>a.name===v.name&&i!==editing))throw new Error(t("Action 名称重复")); const next = [...items]; next[editing!] = { ...v, parameters: p }; setItems(next); setEditing(undefined); } catch (e) { setError((e as Error).message); } };
 const save = async () => {
  setBusy(true); setError('');
  try {
   await managementCall('configure_actions', { database_id: db.id, database: { ...db, actions: items, discovery: undefined } });
   onSaved(); message.success(t('业务操作配置已保存'));
  } catch (e) { setError((e as Error).message); } finally { setBusy(false); }
 };
 const trial = async () => {
  setBusy(true); setError(''); setResult(undefined);
  try { const arguments_ = JSON.parse(args); if (!arguments_ || Array.isArray(arguments_) || typeof arguments_ !== 'object') throw new Error(t('调用参数必须是 JSON 对象')); setResult(await managementCall('try_action', { database_id: db.id, name: trialName, business_user_id: identity, arguments: arguments_, request_id: crypto.randomUUID() })); }
  catch (e) { setError((e as Error).message); } finally { setBusy(false); }
 };
 const dirty = JSON.stringify(items) !== JSON.stringify(db.actions || []);
 return <Card title={t('业务操作 Action')} extra={<Button type="primary" loading={busy} disabled={!canWrite || !dirty} onClick={() => void save()}>{t('保存业务操作')}</Button>}>
  <Space orientation="vertical" style={{ width: '100%' }} size="middle">
   {error && <Alert type="error" showIcon title={error} />}
   {!db.write_enabled&&<Alert type="info" title="写入连接尚未启用" description={<a href="#/database">在数据库配置中启用写入连接后，才能试运行与执行 Action。</a>}/>}
   <Input.Search aria-label="搜索 Action" placeholder="搜索名称、标题、说明" allowClear value={search} onChange={e=>setSearch(e.target.value)}/>
   <Typography.Paragraph type="secondary">{t('每次操作在独立事务中执行。my.one / my.all 沿用查询权限；db.one / db.all 读取可信数据，db.exec 写入。SQL 只接受模板写法。确认操作先预览并回滚，再由业务账号本人打开确认页登录执行。')}</Typography.Paragraph>
   <Button disabled={!canWrite} onClick={() => edit(items.length)}>{t('新增业务操作')}</Button>
   <Table rowKey="name" scroll={{x:900}} pagination={{pageSize:12,showSizeChanger:true,showTotal:n=>`共 ${n} 个 Action`}} dataSource={items.filter(a=>`${a.name} ${a.title} ${a.description}`.toLowerCase().includes(search.toLowerCase()))} columns={[
    { title: t('名称'), dataIndex: 'name' }, { title: t('标题'), dataIndex: 'title' }, { title: t('说明'), dataIndex: 'description' }, { title: t('人工确认'), render: (_, a: ActionDefinition) => a.confirm ? t('是') : t('否') },
    { title: t('操作'), render: (_, a: ActionDefinition) => {const i=items.indexOf(a);return <Space><Button disabled={!canWrite} onClick={() => edit(i)}>{t('编辑')}</Button><Button disabled={!canWrite} onClick={()=>{const copy={...a,name:a.name+'_copy',title:a.title+' 副本'};form.setFieldsValue(copy);setParameters(parameterRows(a.parameters));setEditing(items.length);}}>复制</Button><Button danger disabled={!canWrite} onClick={() => Modal.confirm({ title: t('删除业务操作？'), onOk: () => setItems(items.filter((_, j) => j !== i)) })}>{t('删除')}</Button><Button disabled={dirty || !db.write_enabled || !db.permissions?.includes('impersonate') || !canWrite} onClick={() => { setTrialName(a.name); setResult(undefined); }}>{t('试运行')}</Button></Space>} },
   ]} />
   <Modal className="action-dialog" styles={{ body: { maxHeight: '72vh', overflowY: 'auto' } }} title={t('编辑业务操作')} open={editing !== undefined} width={900} onCancel={() => setEditing(undefined)} onOk={() => void apply()}>
    {error && <Alert type="error" title={error} />}
    <Form form={form} layout="vertical"><Form.Item name="name" label={t('名称')} rules={[{ required: true, pattern: /^[a-zA-Z][a-zA-Z0-9_]{0,63}$/ }]}><Input /></Form.Item><Form.Item name="title" label={t('标题')} rules={[{ required: true }]}><Input /></Form.Item><Form.Item name="description" label={t('说明')} rules={[{ required: true }]}><Input /></Form.Item><Form.Item name="confirm" label={t('人工确认')} valuePropName="checked"><Switch /></Form.Item><Form.Item label={t('参数定义')}><ParameterEditor value={parameters} onChange={setParameters}/></Form.Item><Form.Item name="script" label="JavaScript · run(args, ctx)" rules={[{ required: true }]}><CodeEditor/></Form.Item></Form>
   </Modal>
   <Modal className="action-dialog" styles={{ body: { maxHeight: '72vh', overflowY: 'auto' } }} title={t('试运行（始终回滚）')} open={!!trialName} width={900} onCancel={() => setTrialName(undefined)} onOk={() => void trial()} confirmLoading={busy} okText={t('执行预览')}>
    {error && <Alert type="error" title={error} />}<Space orientation="vertical" style={{ width: '100%' }}><Select style={{ width: 320 }} value={trialName} onChange={setTrialName} options={db.actions?.map(a => ({ value: a.name, label: a.title }))} /><Typography.Text>{t('业务身份编号')}</Typography.Text><InputNumber min={1} precision={0} value={identity} onChange={v => setIdentity(v || 1)} /><Typography.Text>{t('调用参数 JSON')}</Typography.Text><CodeEditor language="json" height="180px" value={args} onChange={setArgs}/>{result !== undefined && <pre style={{ whiteSpace: 'pre-wrap', maxHeight: 400, overflow: 'auto' }}>{JSON.stringify(result, null, 2)}</pre>}</Space>
   </Modal>
  </Space>
 </Card>;
}

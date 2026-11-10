import { useEffect, useRef, useState } from 'react';
import { CopyOutlined, DeleteOutlined, PlayCircleOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons';
import { Alert, Button, Card, Empty, Form, Input, InputNumber, Modal, Select, Space, Switch, Tag, Tooltip, Typography, message } from 'antd';
import { databases, managementCall, type Database, type ActionDefinition } from '../api';
import { ActionLibraries } from '../components/ActionLibraries';
import { DraftAssistant } from '../components/ActionAssistant';
import { CodeEditor } from '../components/CodeEditor';
import { PublicationSwitch } from '../components/PublicationSwitch';
import { ActionConfirmationHelp } from '../components/ActionConfirmationHelp';
import { IconButton, SaveButton } from '../components/IconButton';
import { UserGuide } from '../components/UserGuide';
import { HelpTip } from '../components/HelpTip';
import { ParameterEditor, parameterMap, parameterRows, type ParameterRow } from '../components/ParameterEditor';
import { useI18n } from '../ui/preferences';
import { selectResource, useNavigationBlocker, useRoute } from '../navigation';

const example: ActionDefinition = {
 name: '', title: '', description: '', confirm: true, published: false,
 parameters: { id: { type: 'integer', description: '记录编号' }, value: { type: 'string', description: '新值', max_length: 200 } },
 script: 'function run(args, ctx) {\n  const row = my.one`SELECT id FROM records WHERE id = ${args.id}`;\n  if (!row) deny("记录不存在或无权查看");\n  const n = db.exec`UPDATE records SET value = ${args.value} WHERE id = ${args.id} AND owner_id = ${ctx.userId}`;\n  if (n !== 1) deny("记录已变化，请重试");\n  return { id: args.id, updated: n };\n}',
};
type ActionFields = Omit<ActionDefinition, 'parameters'>;
const fieldsOf = (action: ActionFields): ActionFields => ({ published: action.published !== false, view_id: action.view_id, name: action.name, title: action.title, description: action.description, confirm: action.confirm, script: action.script });

export function Actions({ db, onSaved }: { db: Database; onSaved: () => void }) {
 const t = useI18n();
 const route = useRoute();
 const [currentDB, setCurrentDB] = useState(db);
 const [selectedName, setSelectedName] = useState<string | undefined>(route.selection);
 const [creating, setCreating] = useState(false);
 const [draft, setDraft] = useState<ActionFields>();
 const [parameters, setParameters] = useState<ParameterRow[]>([]);
 const [search, setSearch] = useState('');
 const [busy, setBusy] = useState(false);
 const [publishing, setPublishing] = useState<string>();
 const [error, setError] = useState('');
 const [remoteChanged, setRemoteChanged] = useState(false);
 const [trialName, setTrialName] = useState<string>();
 const [trialBusy, setTrialBusy] = useState(false);
 const [trialError, setTrialError] = useState('');
 const [identity, setIdentity] = useState(db.business_user_id || 1);
 const [args, setArgs] = useState('{}');
 const [result, setResult] = useState<unknown>();
 const [form] = Form.useForm<ActionFields>();
 const listRef = useRef<HTMLUListElement>(null);
 const items = currentDB.actions || [];
 const selected = items.find(action => action.name === selectedName);
 const canWrite = !!currentDB.permissions?.includes('action.write');
 const dirty = !!draft && (creating || !selected || JSON.stringify(fieldsOf(draft)) !== JSON.stringify(fieldsOf(selected)) || JSON.stringify(parameters) !== JSON.stringify(parameterRows(selected.parameters)));
 const locked = busy || trialBusy;
 useNavigationBlocker(dirty,t('放弃当前动作的未保存修改？'),locked);

 const edit = (action?: ActionDefinition, isNew = false, replace = true) => {
  setSelectedName(isNew ? undefined : action?.name); setCreating(isNew);
  setDraft(action ? fieldsOf(action) : undefined); setParameters(parameterRows(action?.parameters || {}));
  form.resetFields(); if (action) form.setFieldsValue(fieldsOf(action));
  setError(''); setTrialName(undefined); setTrialError(''); setResult(undefined); setArgs('{}');
  if (!isNew) selectResource(action?.name,replace);
 };
 // A parent refresh must not erase an action being edited or replace the
 // revision against which its draft was created.
 useEffect(() => {
  if (db.id !== currentDB.id || !draft && !creating) {
   setCurrentDB(db); setRemoteChanged(false); edit(db.actions?.find(action=>action.name===route.selection)||db.actions?.[0]); return;
  }
  if (db.revision <= currentDB.revision) return;
  if (dirty) { setRemoteChanged(true); return; }
  setCurrentDB(db); setRemoteChanged(false); edit(db.actions?.find(action => action.name === selectedName) || db.actions?.[0]);
 }, [db.id, db.revision]);
 useEffect(()=>{
  if(route.selection===selectedName)return;
  edit(items.find(action=>action.name===route.selection)||items[0]);
 },[route.selection]);
 useEffect(() => {
  if (!dirty) return;
  const warn = (event: BeforeUnloadEvent) => { event.preventDefault(); event.returnValue = ''; };
  window.addEventListener('beforeunload', warn);
  return () => window.removeEventListener('beforeunload', warn);
 }, [dirty]);
 useEffect(() => {
  const frame = window.requestAnimationFrame(() => listRef.current?.querySelector('.selected')?.scrollIntoView({ block: 'nearest' }));
  return () => window.cancelAnimationFrame(frame);
 }, [selectedName, creating]);
 const navigate = (work: () => void | Promise<void>) => {
  if (locked) return;
  if (dirty) Modal.confirm({ title: t('放弃当前动作的未保存修改？'), onOk: work });
  else void work();
 };
 const persist = async (actions: ActionDefinition[], nextName?: string) => {
  setBusy(true); setError('');
  try {
   const next = await managementCall<Database>('configure_actions', { database_id: currentDB.id, database: { ...currentDB, actions, discovery: undefined } });
   setCurrentDB(next); setRemoteChanged(false);
   const active = next.actions?.find(action => action.name === nextName) || next.actions?.[0];
   edit(active);
   if (active && !`${active.name} ${active.title} ${active.description}`.toLowerCase().includes(search.toLowerCase())) setSearch('');
   onSaved(); message.success(t('业务操作配置已保存'));
  } catch (e) { setError((e as Error).message); } finally { setBusy(false); }
 };
 const save = async () => {
  try {
   const values = await form.validateFields();
   const definition: ActionDefinition = { ...values, parameters: parameterMap(parameters) };
   if (items.some(action => action.name === values.name && (creating || action.name !== selectedName))) throw new Error(t('Action 名称重复'));
   const next = creating ? [...items, definition] : items.map(action => action.name === selectedName ? definition : action);
   await persist(next, definition.name);
  } catch (e) { if (e instanceof Error) setError(e.message); }
 };
 const publish = async (item: ActionDefinition, published: boolean) => {
  if (locked || !canWrite || remoteChanged) return;
  setBusy(true); setPublishing(item.name); setError('');
  try {
   const next = await managementCall<Database>('configure_actions', { database_id: currentDB.id, database: { ...currentDB, discovery: undefined, actions: items.map(action => action.name === item.name ? { ...action, published } : action) } });
   setCurrentDB(next); setRemoteChanged(false);
   if (!creating && selectedName === item.name) { setDraft(value => value && { ...value, published }); form.setFieldValue('published', published); }
   onSaved(); message.success(t(published ? '已发布' : '已取消发布'));
  } catch(e) { setError((e as Error).message); } finally { setBusy(false); setPublishing(undefined); }
 };
 const copy = () => {
  if (!selected) return;
  let index = 1; let name: string;
  do { const suffix = `_copy${index === 1 ? '' : index}`; name = selected.name.slice(0, 64 - suffix.length) + suffix; index++; } while (items.some(action => action.name === name));
  setSearch(''); edit({ ...selected, name, title: `${selected.title} ${t('副本')}`, published: false }, true);
 };
 const remove = () => Modal.confirm({
  title: t('删除动作 {0}？', { '0': draft?.title || draft?.name || t('未命名动作') }),
  content: dirty ? t('当前未保存的修改也会放弃。') : undefined,
  onOk: () => creating ? edit(items[0]) : persist(items.filter(action => action.name !== selectedName), items.find(action => action.name !== selectedName)?.name),
 });
 const reload = () => navigate(async () => {
  setBusy(true); setError('');
  try { const next = await databases.get(currentDB.id); setCurrentDB(next); setRemoteChanged(false); edit(next.actions?.find(action => action.name === selectedName) || next.actions?.[0]); onSaved(); }
  catch (e) { setError((e as Error).message); } finally { setBusy(false); }
 });
 const trial = async () => {
  setTrialBusy(true); setTrialError(''); setResult(undefined);
  try {
   const arguments_ = JSON.parse(args);
   if (!arguments_ || Array.isArray(arguments_) || typeof arguments_ !== 'object') throw new Error(t('调用参数必须是 JSON 对象'));
   setResult(await managementCall('try_action', { database_id: currentDB.id, name: trialName, business_user_id: identity, arguments: arguments_, request_id: crypto.randomUUID() }));
  } catch (e) { setTrialError((e as Error).message); } finally { setTrialBusy(false); }
 };
 const visible = items.filter(action => `${action.name} ${action.title} ${action.description}`.toLowerCase().includes(search.toLowerCase()));
 const canTrial = !!selected && !creating && !dirty && !!currentDB.write_enabled && !!currentDB.permissions?.includes('impersonate') && canWrite;
 return <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
  <div className="action-workspace">
   <Card className="action-list-panel" title={<Space>{t('动作列表')}<Tag>{items.length}</Tag></Space>} extra={<ActionLibraries db={currentDB} disabled={dirty || locked} onSaved={next => { setCurrentDB(next); onSaved(); }} />}>
    <div className="action-list-toolbar">
     <Input aria-label={t('搜索动作')} placeholder={t('搜索名称、标题、说明')} value={search} allowClear onChange={event => setSearch(event.target.value)} />
     <Tooltip title={t('新增动作')}><Button type="primary" aria-label={t('新增动作')} icon={<PlusOutlined />} disabled={!canWrite || locked || creating} onClick={() => navigate(() => { setSearch(''); edit(example, true); })} /></Tooltip>
    </div>
    <ul ref={listRef} className="action-list" aria-label={t('动作列表')}>
     {creating && <li><button type="button" className="action-list-item selected" aria-current="true"><strong>{draft?.title || t('未命名动作')}</strong><span>{draft?.name || t('新动作')}</span><Tag color="orange">{t('未保存')}</Tag></button></li>}
     {visible.map(action => <li key={action.name} className="resource-list-row"><button type="button" className={`action-list-item${!creating && selectedName === action.name ? ' selected' : ''}`} aria-current={!creating && selectedName === action.name ? 'true' : undefined} disabled={locked} onClick={() => { if (creating || action.name !== selectedName) navigate(() => edit(action,false,false)); }}>
      <strong>{action.title}</strong><span>{action.name}</span>
     </button><PublicationSwitch name={action.title} published={action.published !== false} disabled={!canWrite || locked || remoteChanged} loading={publishing === action.name} onChange={published => void publish(action, published)} /></li>)}
     {!visible.length && !creating && <li><Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('暂无动作')} /></li>}
    </ul>
   </Card>
   <Card className="action-editor-panel" title={<Space>{creating ? t('新增动作') : selected?.title || t('选择动作')}{dirty && <Tag color="orange">{t('未保存')}</Tag>}</Space>} extra={<Space wrap>
    {draft && <Space wrap>
    <IconButton label={t('试运行')} help={dirty ? t('保存后可试运行') : undefined} icon={<PlayCircleOutlined />} disabled={!canTrial || locked} onClick={() => { setTrialName(selectedName); setResult(undefined); setTrialError(''); }} />
    <IconButton label={t('复制')} icon={<CopyOutlined />} disabled={!canWrite || dirty || creating || locked} onClick={copy} />
    <IconButton label={t('删除')} icon={<DeleteOutlined />} danger disabled={!canWrite || locked} onClick={remove} />
    <SaveButton loading={busy} disabled={!canWrite || !dirty || trialBusy || remoteChanged} onClick={() => void save()} />
    </Space>}
   </Space>}>
    <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
     {(error || remoteChanged) && <Alert type={error ? 'error' : 'warning'} showIcon title={error || t('配置已更新，请重新载入后继续。')} action={<IconButton label={t('刷新')} icon={<ReloadOutlined />} onClick={reload} />} />}
     {draft && canWrite && currentDB.permissions?.includes('config.read') && !locked && !remoteChanged &&
      <DraftAssistant key={JSON.stringify([currentDB.id, selectedName, creating])} db={currentDB} name={creating ? undefined : selectedName} draft={draft} rows={parameters}
       apply={(script, params) => {
        if (script !== undefined) { form.setFieldValue('script', script); setDraft({ ...draft, script }); }
        if (params) setParameters(parameterRows(params));
       }} />}
     {draft ? <Form form={form} layout="vertical" disabled={!canWrite || locked} onValuesChange={(_, values) => setDraft(values)}>
      <Form.Item name="published" valuePropName="checked" hidden><Switch /></Form.Item>
      <div className="action-fields-grid">
       <Form.Item name="name" label={t('名称')} rules={[{ required: true, pattern: /^[a-zA-Z][a-zA-Z0-9_]{0,63}$/ }]}><Input /></Form.Item>
       <Form.Item name="title" label={t('标题')} rules={[{ required: true }]}><Input /></Form.Item>
       <Form.Item name="description" label={t('说明')} rules={[{ required: true }]}><Input.TextArea rows={3} /></Form.Item>
       <Form.Item name="view_id" label={t("关联视图")}><Select allowClear placeholder={t("无关联视图")} options={(currentDB.views || []).filter(v => v.published).map(v => ({ value:v.id, label:v.name }))} /></Form.Item>
       <Form.Item name="confirm" label={<Space size={4}>{t('人工确认')}<ActionConfirmationHelp /></Space>} valuePropName="checked"><Switch /></Form.Item>
      </div>
      <Form.Item name="script" label={<Space size={4}>JavaScript · run(args, ctx)<HelpTip label={t('动作代码')}>{t('my.one / my.all 按查询权限读取，db.exec 使用 SQL 模板写入。人工确认动作先预览，再由用户确认执行。')}{' '}{t('可用 import { fn1, fn2 } from "库名称" 引用当前数据库的公共函数。')}</HelpTip><UserGuide scriptReference/></Space>} rules={[{ required: true }]}><CodeEditor height="380px" readOnly={!canWrite || locked} /></Form.Item>
      <Form.Item label={t('参数定义')}><ParameterEditor value={parameters} onChange={setParameters} readOnly={!canWrite || locked} /></Form.Item>
     </Form> : <Empty description={t('从左侧选择动作，或新增一个动作。')} />}
    </Space>
   </Card>
  </div>
  <Modal className="action-dialog" title={t('试运行（始终回滚）')} open={!!trialName} width={900} onCancel={() => { if (!trialBusy) setTrialName(undefined); }} onOk={() => void trial()} confirmLoading={trialBusy} okText={t('执行预览')} styles={{ body: { maxHeight: '72vh', overflowY: 'auto' } }}>
   {trialError && <Alert type="error" title={trialError} style={{ marginBottom: 12 }} />}
   <Space orientation="vertical" style={{ width: '100%' }}>
    <Typography.Text strong>{items.find(action => action.name === trialName)?.title}</Typography.Text>
    <div><Typography.Text>{t('业务身份编号')}</Typography.Text><InputNumber aria-label={t('业务身份编号')} style={{ marginLeft: 12 }} min={1} precision={0} value={identity} disabled={trialBusy} onChange={value => setIdentity(value || 1)} /></div>
    <Typography.Text>{t('调用参数 JSON')}</Typography.Text><CodeEditor language="json" height="180px" value={args} onChange={setArgs} readOnly={trialBusy} />
    {result !== undefined && <pre className="audit-detail">{JSON.stringify(result, null, 2)}</pre>}
   </Space>
  </Modal>
 </Space>;
}

import { useEffect, useRef, useState } from 'react';
import { PlusOutlined, ReloadOutlined, PlayCircleOutlined, DeleteOutlined, ClockCircleOutlined, HistoryOutlined, ApiOutlined } from '@ant-design/icons';
import { Alert, Button, Card, Col, Collapse, DatePicker, Descriptions, Empty, Form, Input, InputNumber, Modal, Row, Select, Space, Switch, Table, Tabs, Tag, Typography, message } from 'antd';
import dayjs from 'dayjs';
import utc from 'dayjs/plugin/utc';
import timezone from 'dayjs/plugin/timezone';
import { type Database, type TableSchema } from '../api';
import { sourceCall, newImportTask, type ImportTask, type SourceConnection, type SourceOverview, type ImportRun } from '../dataSources';
import { ConnectionFields, driverLabels } from '../components/ConnectionFields';
import { isMySQLDriver } from '../components/connectionURL';
import { CodeEditor } from '../components/CodeEditor';
import { IconButton, SaveButton } from '../components/IconButton';
import { DatabaseIcon } from '../components/DatabaseIcon';
import { ImportRunDetails, importRunStatus } from '../components/ImportRuns';
import { navigate, selectResource, setRouteParams, useNavigationBlocker, useRoute } from '../navigation';
import { useI18n } from '../ui/preferences';

dayjs.extend(utc);
dayjs.extend(timezone);
const stamp = (value?: string) => value ? new Date(value).toLocaleString() : '—';
function previewTimes(task: ImportTask): string[] {
 const s = task.schedule;
 const now = dayjs();
 if (s.mode === 'once') return s.at && dayjs(s.at).isValid() ? [dayjs(s.at).tz(s.timezone).format('YYYY-MM-DD HH:mm:ss Z')] : [];
 if (s.mode === 'interval') return [1, 2, 3].map(i => now.add(i * (s.minutes || 30), 'minute').tz(s.timezone).format('YYYY-MM-DD HH:mm:ss Z'));
 if (s.mode === 'daily' && s.time) {
  const result: string[] = [];
  for (let i = 0; i < 4 && result.length < 3; i++) {
   const candidate = dayjs.tz(`${now.tz(s.timezone).add(i, 'day').format('YYYY-MM-DD')} ${s.time}`, s.timezone);
   if (candidate.isAfter(now)) result.push(candidate.format('YYYY-MM-DD HH:mm:ss Z'));
  }
  return result;
 }
 return [];
}
const cleanTask = (task: ImportTask) => JSON.stringify({ ...task, last_run: undefined, next_run: undefined, checkpoint: undefined, owner: undefined });
const objectJSON = (text: string): Record<string, unknown> => {
 const value: unknown = JSON.parse(text);
 if (!value || Array.isArray(value) || typeof value !== 'object') throw new Error('JSON object required');
 return value as Record<string, unknown>;
};

export default function DataSources({ db }: { db: Database }) {
 const t = useI18n();
 const route = useRoute();
 const [data, setData] = useState<SourceOverview>({ connections: [], tasks: [], runs: [] });
 const [loading, setLoading] = useState(true);
 const [busy, setBusy] = useState(false);
 const [error, setError] = useState('');
 const [search, setSearch] = useState('');
 const [draft, setDraft] = useState<ImportTask>();
 const [baseline, setBaseline] = useState('');
 const [parameters, setParameters] = useState('{}');
 const [connection, setConnection] = useState<SourceConnection>();
 const [connectionOpen, setConnectionOpen] = useState(false);
 const [connectionsOpen, setConnectionsOpen] = useState(route.params.get('sourceTab') === 'connections');
 const [connectionError, setConnectionError] = useState('');
 const [connectionResult, setConnectionResult] = useState<string>();
 const [details, setDetails] = useState<string>();
 const [schemaSource, setSchemaSource] = useState<string>();
 const [tables, setTables] = useState<string[]>([]);
 const [schema, setSchema] = useState<TableSchema>();
 const [runTask, setRunTask] = useState<ImportTask>();
 const [runTrial, setRunTrial] = useState(false);
 const [runParameters, setRunParameters] = useState('{}');
 const [form] = Form.useForm();
 const live = useRef(true);
 const refreshing = useRef(false);
 const creating = route.params.get('newTask') === '1';
 const taskTab = route.params.get('taskTab') || 'script';
 const dirty = !!draft && (cleanTask(draft) !== baseline || parameters !== JSON.stringify(draft.parameters || {}, null, 2));
 useNavigationBlocker(dirty, t('放弃数据来源的未保存修改？'), busy);
 const call = <T,>(action: string, args: Record<string, unknown> = {}) => sourceCall<T>(db.id, action, args);
 const refresh = async (initial = false) => {
  if (refreshing.current) return;
  refreshing.current = true;
  try { const next = await call<SourceOverview>('list_data_sources'); if (live.current) { setData(next); if (initial) setError(''); } }
  catch (e) { if (live.current) setError((e as Error).message); }
  finally { refreshing.current = false; if (live.current) setLoading(false); }
 };
 useEffect(() => {
  live.current = true; void refresh(true);
  const timer = window.setInterval(() => void refresh(), 3000);
  return () => { live.current = false; window.clearInterval(timer); };
 }, [db.id]);
 const selected = creating ? undefined : route.selection ? data.tasks.find(task => task.id === route.selection) : data.tasks[0];
 useEffect(() => { if (route.params.get('sourceTab') === 'connections') setConnectionsOpen(true); }, [route.params.get('sourceTab')]);
 useEffect(() => {
  if (route.params.get('newTask') === '1') { const task = newImportTask(); setDraft(task); setBaseline(cleanTask(task)); setParameters('{}'); return; }
  if (selected) { setDraft({ ...selected }); setBaseline(cleanTask(selected)); setParameters(JSON.stringify(selected.parameters || {}, null, 2)); }
  else setDraft(undefined);
 }, [route.selection, selected?.id, route.params.get('newTask')]);
 useEffect(() => { if (!dirty && selected && selected.revision !== draft?.revision) { setDraft({ ...selected }); setBaseline(cleanTask(selected)); setParameters(JSON.stringify(selected.parameters || {}, null, 2)); } }, [selected?.revision]);
 const work = async (fn: () => Promise<unknown>, text?: string) => {
  setBusy(true); setError('');
  try { await fn(); await refresh(); if (text) message.success(text); }
  catch (e) { setError((e as Error).message); }
  finally { setBusy(false); }
 };
 const selectTask = (id: string) => navigate('sources', id, { database: db.id });
 const patch = (values: Partial<ImportTask>) => setDraft(old => old && ({ ...old, ...values }));
 const saveTask = () => void work(async () => {
  if (!draft) return;
  let params: Record<string, unknown>; try { params = objectJSON(parameters); } catch { throw new Error(t('参数须为 JSON 对象')); }
  const saved = await call<ImportTask>('save_import_task', { import_task: { ...draft, parameters: params, last_run: undefined } });
  setDraft(saved); setBaseline(cleanTask(saved)); setParameters(JSON.stringify(saved.parameters || {}, null, 2));
  // Save has explicitly resolved the draft before updating the bookmark.
  setRouteParams({ taskTab: undefined, newTask: undefined }, true, true);
  selectResource(saved.id);
 }, t('任务已保存'));
 const editConnection = (value?: SourceConnection) => {
  setConnection(value); setConnectionError(''); setConnectionResult(undefined); setConnectionOpen(true);
  form.resetFields(); form.setFieldsValue(value ? { ...value, password: '' } : { driver: 'mysql', revision: 0, create_sqlite: false, connection: { port: 3306, tls_mode: 'verify-full' } });
 };
 const connectionValues = async () => {
  const values = await form.validateFields();
  const c = { ...values, revision: connection?.revision || 0, password: undefined, create_sqlite: undefined };
  if (c.driver === 'sqlite') c.connection = undefined;
  else { c.path = undefined; if (isMySQLDriver(c.driver)) c.connection.schema = c.connection.database || ''; }
  return { c, password: values.password };
 };
 const connectionWork = async (save: boolean) => {
  setBusy(true); setConnectionError(''); setConnectionResult(undefined);
  try {
   const { c, password } = await connectionValues();
   if (save) { await call('save_source_connection', { source_connection: c, password }); setConnectionOpen(false); await refresh(); message.success(t('外部连接已保存')); }
   else { const result = await call<{ tables: string[] }>('inspect_source_connection', { source_connection: c, password }); setConnectionResult(t('连接成功，发现 {0} 张表。', { '0': result.tables.length })); }
  } catch (e) { if (e instanceof Error) setConnectionError(e.message); }
  finally { setBusy(false); }
 };
 const inspect = (id: string, table?: string) => void work(async () => {
  if (!table) { setSchemaSource(id); setSchema(undefined); setTables([]); const result = await call<{ tables: string[] }>('inspect_source_connection', { id }); setTables(result.tables); }
  else setSchema(await call<TableSchema>('inspect_source_connection', { id, table }));
 });
 const openRun = (task: ImportTask, trial: boolean) => { setRunTask(task); setRunTrial(trial); setRunParameters(JSON.stringify(task.parameters || {}, null, 2)); };
 const startRun = () => void work(async () => {
  let params: Record<string, unknown>; try { params = objectJSON(runParameters); } catch { throw new Error(t('参数须为 JSON 对象')); }
  const run = await call<ImportRun>(runTrial ? 'trial_import_task' : 'run_import_task', { id: runTask!.id, parameters: params });
  setRunTask(undefined); setDetails(run.id);
 });
 const status = (run?: ImportRun) => importRunStatus(run, t);
 const scheduleLabel = (task: ImportTask) => task.schedule.mode === 'manual' ? t('仅手动') : task.schedule.mode === 'once' ? stamp(task.schedule.at) : task.schedule.mode === 'interval' ? t('每 {0} 分钟', { '0': task.schedule.minutes || 30 }) : `${t('每天')} ${task.schedule.time}`;

 return <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
  {db.driver !== 'sqlite' && !db.write_enabled && <Alert type="warning" showIcon title={t('网络目标库需要先在动作配置中启用写入并配置独立写入账号。')} />}
  {error && <Alert type="error" showIcon title={t('操作失败')} description={error} closable onClose={() => setError('')} />}
  <div className="action-workspace source-workspace">
   <Card className="action-list-panel" title={<Space>{t('导入列表')}<Tag>{data.tasks.length}</Tag></Space>} extra={<IconButton label={t('外部连接')} icon={<ApiOutlined />} disabled={busy} onClick={() => setConnectionsOpen(true)} />}>
    <div className="action-list-toolbar">
     <Input aria-label={t('搜索任务')} placeholder={t('搜索名称、标题、说明')} value={search} allowClear onChange={e => setSearch(e.target.value)} />
     <IconButton type="primary" label={t('新建任务')} icon={<PlusOutlined />} disabled={busy || creating} onClick={() => { setSearch(''); navigate('sources', undefined, { newTask: '1' }); }} />
     <IconButton label={t('刷新')} icon={<ReloadOutlined />} loading={loading} disabled={busy} onClick={() => void refresh()} />
    </div>
    <ul className="action-list" aria-label={t('导入列表')}>
     {creating && <li><button type="button" className="action-list-item selected" aria-current="true"><strong>{draft?.name || t('新建任务')}</strong><span>{draft?.id || t('未保存')}</span><Tag color="orange">{t('未保存')}</Tag></button></li>}
     {data.tasks.filter(task => `${task.name} ${task.id} ${task.description}`.toLowerCase().includes(search.toLowerCase())).map(task => <li key={task.id} className="resource-list-row">
      <button type="button" className={`action-list-item${!creating && selected?.id === task.id ? ' selected' : ''}`} aria-current={!creating && selected?.id === task.id ? 'true' : undefined} disabled={busy} onClick={() => { if (creating || selected?.id !== task.id) selectTask(task.id); }}>
       <strong>{task.name}</strong><span>{task.id}</span><span><ClockCircleOutlined /> {scheduleLabel(task)}</span>
      </button>
      <span className="resource-publication"><Switch size="small" aria-label={t('自动运行 · {0}', { '0': task.name })} checked={task.enabled} disabled={busy || dirty || task.schedule.mode === 'manual'} onChange={enabled => void work(() => call('save_import_task', { import_task: { ...task, enabled, last_run: undefined } }))} /></span>
     </li>)}
     {!data.tasks.length && !creating && <li><Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('暂无导入任务')} /></li>}
     {!!data.tasks.length && !creating && !data.tasks.some(task => `${task.name} ${task.id} ${task.description}`.toLowerCase().includes(search.toLowerCase())) && <li><Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('没有匹配的导入任务')} /></li>}
    </ul>
   </Card>
   <Card className="action-editor-panel source-task-editor" title={<Space>{t('导入详情')}{dirty && <Tag color="orange">{t('未保存')}</Tag>}</Space>} extra={draft && <Space wrap>
    <IconButton label={t('数据运行记录')} icon={<HistoryOutlined />} disabled={busy || !draft.revision} onClick={() => navigate('data-runs', undefined, { task: draft.id })} />
    <IconButton label={t('回滚试运行')} icon={<PlayCircleOutlined />} disabled={busy || dirty || !draft.revision} onClick={() => openRun(draft, true)} />
    <Button icon={<PlayCircleOutlined />} size="small" disabled={busy || dirty || !draft.revision} onClick={() => openRun(draft, false)}>{t('立即运行')}</Button>
    <IconButton label={t('删除任务')} icon={<DeleteOutlined />} danger disabled={busy || !draft.revision || selected?.last_run?.status === 'running'} onClick={() => Modal.confirm({ title: t('删除任务？'), onOk: () => work(async () => { await call('delete_import_task', { id: draft.id }); setDraft(undefined); setBaseline(''); selectResource(undefined); }) })} />
    <SaveButton loading={busy} disabled={!dirty && !!draft.revision} onClick={saveTask} />
   </Space>}>
    {draft ? <>
     {selected && selected.revision !== draft.revision && <Alert type="warning" title={t('任务已被其他窗口修改，请保留草稿后刷新。')} />}
     <Form className="configuration-form" layout="horizontal" labelAlign="left" labelWrap disabled={busy}>
      <Row gutter={16}><Col xs={24} md={12}><Form.Item label={<span className="configuration-label-text">{t('任务名称')}</span>} required><Input value={draft.name} maxLength={200} onChange={e => patch({ name: e.target.value })} /></Form.Item></Col>
       <Col xs={24} md={12}><Form.Item label={<span className="configuration-label-text">{t('任务标识')}</span>} required help={t('以字母开头，使用字母、数字、下划线或短横线。保存后固定。')}><Input value={draft.id} disabled={!!draft.revision} maxLength={64} onChange={e => patch({ id: e.target.value })} /></Form.Item></Col></Row>
      <Tabs activeKey={taskTab} onChange={value => setRouteParams({ taskTab: value }, false, true)} items={[
       { key: 'script', label: t('脚本'), children: <>
        <Form.Item label={<span className="configuration-label-text">{t('来源连接')}</span>} extra={<Button type="link" size="small" icon={<PlusOutlined />} onClick={() => editConnection()}>{t('新增外部连接')}</Button>}><Select mode="multiple" value={draft.sources} options={data.connections.map(c => ({ value: c.id, label: `${c.name} (${c.id})` }))} onChange={sources => patch({ sources })} /></Form.Item>
        <div className="source-script-layout"><div><CodeEditor value={draft.script} onChange={script => patch({ script })} height="480px" readOnly={busy} /><Typography.Paragraph type="secondary" style={{ marginTop: 8 }}>{t('所有转换、分页和写入逻辑均在 JS 中编写。目标表须预先存在。')}</Typography.Paragraph></div>
         <Collapse size="small" defaultActiveKey={['api']} items={[
          { key: 'api', label: t('脚本接口'), children: <div className="source-api-reference">
           <code>function main(params, context)</code><p>{t('同步入口，无网络、文件或 npm 能力。')}</p>
           <code>sources.get("alias").query(sql, [])</code><p>{t('只读查询，使用 ? 绑定值；每次最多 1000 行，超过则报错。')}</p>
           <code>db.target().query(sql, [])</code><code>db.target().update(sql, [])</code><p>{t('当前目标库，支持 SELECT 与受限 INSERT / UPDATE / DELETE。')}</p>
           <code>context.checkpoint</code><code>context.saveCheckpoint(object)</code><p>{t('正式运行成功后保存进度；重跑脚本应能处理重复数据。')}</p>
           <code>context.log(message)</code><code>context.trial / context.now</code>
          </div> },
          { key: 'schema', label: t('来源表结构'), children: <Space orientation="vertical" style={{ width: '100%' }}>
           <Select style={{ width: '100%' }} placeholder={t('选择来源连接')} value={schemaSource} options={draft.sources.map(id => ({ value: id, label: id }))} onChange={id => inspect(id)} />
           <Select style={{ width: '100%' }} placeholder={t('选择数据表')} value={schema?.name} showSearch options={tables.map(name => ({ value: name, label: name }))} onChange={table => schemaSource && inspect(schemaSource, table)} />
           {schema?.columns.map(column => <div key={column.name}><Typography.Text code>{column.name}</Typography.Text> <Typography.Text type="secondary">{column.type}</Typography.Text></div>)}
          </Space> },
         ]} />
        </div>
        <Form.Item label={<span className="configuration-label-text">{t('默认参数（JSON）')}</span>} style={{ marginTop: 16 }}><Input.TextArea rows={4} value={parameters} onChange={e => setParameters(e.target.value)} spellCheck={false} /></Form.Item>
        <Form.Item label={<span className="configuration-label-text">{t('说明')}</span>}><Input.TextArea value={draft.description} onChange={e => patch({ description: e.target.value })} /></Form.Item>
       </> },
       { key: 'schedule', label: t('运行计划'), children: <div style={{ maxWidth: 620 }}>
        <Form.Item label={<span className="configuration-label-text">{t('运行方式')}</span>}><Select value={draft.schedule.mode} options={[{ value: 'manual', label: t('仅手动') }, { value: 'once', label: t('指定时间') }, { value: 'interval', label: t('固定间隔') }, { value: 'daily', label: t('每天运行') }]} onChange={mode => patch({ schedule: { ...draft.schedule, mode }, enabled: mode === 'manual' ? false : draft.enabled })} /></Form.Item>
        {draft.schedule.mode === 'interval' && <Form.Item label={<span className="configuration-label-text">{t('间隔（分钟）')}</span>}><InputNumber min={1} max={525600} value={draft.schedule.minutes} onChange={value => patch({ schedule: { ...draft.schedule, minutes: value || 30 } })} /></Form.Item>}
        {draft.schedule.mode === 'daily' && <Form.Item label={<span className="configuration-label-text">{t('每天运行时间')}</span>}><Input type="time" value={draft.schedule.time} onChange={e => patch({ schedule: { ...draft.schedule, time: e.target.value } })} /></Form.Item>}
        {draft.schedule.mode === 'once' && <Form.Item label={<span className="configuration-label-text">{t('指定时间')}</span>}><DatePicker showTime style={{ width: '100%' }} value={draft.schedule.at ? dayjs(draft.schedule.at).tz(draft.schedule.timezone) : null} onChange={value => patch({ schedule: { ...draft.schedule, at: value ? dayjs.tz(value.format('YYYY-MM-DD HH:mm:ss'), draft.schedule.timezone).toISOString() : undefined } })} /></Form.Item>}
        <Form.Item label={<span className="configuration-label-text">{t('时区')}</span>}><Select showSearch value={draft.schedule.timezone} options={['Asia/Shanghai', 'UTC', 'Asia/Tokyo', 'Europe/London', 'America/New_York'].map(value => ({ value, label: value }))} onChange={timezone => patch({ schedule: { ...draft.schedule, timezone } })} /></Form.Item>
        <Form.Item label={<span className="configuration-label-text">{t('自动运行')}</span>}><Switch checked={draft.enabled} disabled={draft.schedule.mode === 'manual'} onChange={enabled => patch({ enabled })} /></Form.Item>
        <Form.Item label={<span className="configuration-label-text">{t('执行时限（秒）')}</span>}><InputNumber min={1} max={3600} value={draft.timeout} onChange={value => patch({ timeout: value || 300 })} /></Form.Item>
        <Alert type="info" showIcon title={t('同一任务不重叠执行。错过的计划合并执行一次，失败不会自动重试。')} />
        {draft.schedule.mode !== 'manual' && <Card size="small" title={t('预计运行时间')} style={{ marginTop: 16 }}><Typography.Paragraph type="secondary">{draft.schedule.timezone}</Typography.Paragraph>{previewTimes(draft).map(value => <div key={value}>{value}</div>)}<Typography.Text type="secondary">{t('保存并开启自动运行后生效。')}</Typography.Text></Card>}
        {draft.next_run && <Typography.Paragraph style={{ marginTop: 12 }}>{t('已保存的下次运行时间')}：{stamp(draft.next_run)}</Typography.Paragraph>}
       </div> },
       { key: 'progress', label: t('进度与状态'), children: <><Descriptions column={{ xs: 1, md: 2 }} items={[
        { key: 'schedule', label: t('运行计划'), children: scheduleLabel(draft) },
        { key: 'next', label: t('下次运行'), children: stamp(selected?.next_run) },
        { key: 'last', label: t('最近运行'), children: selected?.last_run ? <Button type="text" onClick={() => setDetails(selected.last_run!.id)}>{status(selected.last_run)}</Button> : status() },
        { key: 'started', label: t('开始时间'), children: stamp(selected?.last_run?.started) },
       ]} /><Alert type="info" showIcon title={t('本版整次运行使用一个目标事务；成功后提交并保存进度，失败或停止则回滚。')} /><Typography.Paragraph style={{ marginTop: 16 }}>{t('已保存进度')}</Typography.Paragraph><pre>{JSON.stringify(selected?.checkpoint || {}, null, 2)}</pre></> },
      ]} />
     </Form>
    </> : <Empty description={t('从左侧选择导入任务，或新建一个任务。')} />}
   </Card>
  </div>
  <Modal title={t('外部连接')} open={connectionsOpen} onCancel={() => { if (!busy) { setConnectionsOpen(false); setRouteParams({ sourceTab: undefined }, true, true); } }} width={1080} styles={{ body: { maxHeight: '72vh', overflowY: 'auto' } }} footer={<Button disabled={busy} onClick={() => { setConnectionsOpen(false); setRouteParams({ sourceTab: undefined }, true, true); }}>{t('关闭')}</Button>}>
   {error && <Alert type="error" showIcon title={error} style={{ marginBottom: 16 }} />}
   <div className="source-list-toolbar"><Typography.Text type="secondary">{t('连接可由多个任务复用；来源数据库仅允许读取。')}</Typography.Text><Button type="primary" icon={<PlusOutlined />} disabled={busy} onClick={() => editConnection()}>{t('新增外部连接')}</Button></div>
    <Table<SourceConnection> rowKey="id" loading={loading} dataSource={data.connections} scroll={{ x: 820 }} locale={{ emptyText: t('暂无外部连接') }} columns={[
     { title: t('连接名称'), dataIndex: 'name' }, { title: t('脚本别名'), dataIndex: 'id', render: value => <Typography.Text code>{value}</Typography.Text> },
     { title: t('数据库类型'), render: (_, c) => <Space><DatabaseIcon driver={c.driver} />{t(driverLabels[c.driver] || c.driver)}</Space> },
     { title: t('连接地址'), render: (_, c) => c.path || `${c.connection?.host}:${c.connection?.port}` },
     { title: t('引用任务'), render: (_, c) => data.tasks.filter(task => task.sources.includes(c.id)).map(task => task.name).join('、') || '—' },
     { title: t('操作'), render: (_, c) => <Space><Button size="small" onClick={() => editConnection(c)}>{t('编辑')}</Button><Button size="small" disabled={busy} onClick={() => inspect(c.id)}>{t('表结构')}</Button><Button size="small" danger disabled={busy || data.tasks.some(task => task.sources.includes(c.id))} onClick={() => Modal.confirm({ title: t('删除外部连接？'), onOk: () => work(() => call('delete_source_connection', { id: c.id })) })}>{t('删除')}</Button></Space> },
    ]} />
    {schemaSource && <Card size="small" title={`${t('来源表结构')} · ${schemaSource}`} style={{ marginTop: 16 }}><Select showSearch style={{ minWidth: 240 }} placeholder={t('选择数据表')} value={schema?.name} options={tables.map(name => ({ value: name, label: name }))} onChange={table => inspect(schemaSource, table)} /><Table rowKey="name" size="small" dataSource={schema?.columns || []} pagination={false} style={{ marginTop: 12 }} columns={[{ title: t('字段名'), dataIndex: 'name' }, { title: t('字段类型'), dataIndex: 'type' }, { title: t('说明'), dataIndex: 'comment' }]} /></Card>}
  </Modal>
  <Modal title={t(connection ? '编辑外部连接' : '新增外部连接')} open={connectionOpen} onCancel={() => !busy && setConnectionOpen(false)} width={640} styles={{ body: { maxHeight: '70vh', overflowY: 'auto' } }} footer={<Space><Button disabled={busy} onClick={() => setConnectionOpen(false)}>{t('取消')}</Button><Button loading={busy} onClick={() => void connectionWork(false)}>{t('测试连接')}</Button><Button aria-label={t('保存')} type="primary" loading={busy} onClick={() => void connectionWork(true)}>{t('保存')}</Button></Space>}>
   {connectionError && <Alert type="error" showIcon title={connectionError} style={{ marginBottom: 16 }} />}{connectionResult && <Alert type="success" showIcon title={connectionResult} style={{ marginBottom: 16 }} />}
   <Form className="configuration-form" form={form} layout="horizontal" labelAlign="left" labelWrap onValuesChange={() => setConnectionResult(undefined)}><Form.Item name="name" label={<span className="configuration-label-text">{t('连接名称')}</span>} rules={[{ required: true }]}><Input maxLength={200} /></Form.Item><Form.Item name="id" label={<span className="configuration-label-text">{t('脚本别名')}</span>} rules={[{ required: true }, { pattern: /^[a-zA-Z][a-zA-Z0-9_-]{0,63}$/ }]}><Input disabled={!!connection} placeholder="crm" /></Form.Item><ConnectionFields form={form} external passwordRequired={!connection?.has_password} /></Form>
  </Modal>
  <Modal title={t(runTrial ? '回滚试运行' : '立即运行')} open={!!runTask} onCancel={() => !busy && setRunTask(undefined)} onOk={startRun} confirmLoading={busy} okText={t(runTrial ? '开始试运行' : '开始导入')}>
   <Alert type={runTrial ? 'info' : 'warning'} showIcon title={t(runTrial ? '执行已保存脚本，目标写入全部回滚，不保存正式进度。' : '执行已保存脚本，成功后将数据提交到当前目标数据库。')} style={{ marginBottom: 16 }} />
   <Typography.Paragraph>{runTask?.name}</Typography.Paragraph><Input.TextArea aria-label={t('本次运行参数（JSON）')} rows={7} value={runParameters} onChange={e => setRunParameters(e.target.value)} />
  </Modal>
  <ImportRunDetails databaseID={db.id} runID={details} onClose={() => setDetails(undefined)} />
 </Space>;
}

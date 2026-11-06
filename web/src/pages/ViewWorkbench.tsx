import { useEffect, useRef, useState } from 'react';
import { EditOutlined, PlayCircleOutlined, ReloadOutlined } from '@ant-design/icons';
import { Alert, Card, Empty, Input, Space, Tabs, Tag, Typography } from 'antd';
import { databases, type Database } from '../api';
import BusinessActionWorkbench from '../components/BusinessActionWorkbench';
import { CodeEditor } from '../components/CodeEditor';
import { HelpTip } from '../components/HelpTip';
import { IconButton } from '../components/IconButton';
import { ViewFrame, type ViewData } from '../components/ViewFrame';
import { navigate, selectResource, useRoute, useRouteChoice } from '../navigation';
import { useI18n } from '../ui/preferences';

const initialData = '{\n  "input": {},\n  "result": null\n}';

export default function ViewWorkbench({ db, onSaved }: { db: Database; onSaved: () => void }) {
 const t = useI18n(), route = useRoute();
 const [mode, setMode] = useRouteChoice('mode', 'preview', ['preview', 'business']);
 return <div className="view-workbench"><Tabs key={db.id} activeKey={mode} onChange={setMode} destroyOnHidden tabBarExtraContent={<IconButton label={t('视图配置')} icon={<EditOutlined />} onClick={() => navigate('views', route.selection)} />} items={[
  { key: 'preview', label: t('模拟数据预览'), children: <ViewPreview key={db.id} db={db} onSaved={onSaved} /> },
  { key: 'business', label: t('真实业务预览'), children: <BusinessActionWorkbench key={db.id} db={db} viewsOnly /> },
 ]} /></div>;
}

function ViewPreview({ db, onSaved }: { db: Database; onSaved: () => void }) {
 const t = useI18n(), route = useRoute();
 const [current, setCurrent] = useState(db), [search, setSearch] = useState('');
 const [data, setData] = useState(initialData), [applied, setApplied] = useState<ViewData>();
 const [error, setError] = useState(''), [loading, setLoading] = useState(false), [render, setRender] = useState(0);
 const [detailView, setDetailView] = useRouteChoice('detailView', 'preview', ['preview', 'html']);
 const sequence = useRef(0);
 const items = current.views || [], selected = items.find(v => v.id === route.selection) || items[0];
 const visible = items.filter(v => `${v.id} ${v.name} ${v.description}`.toLowerCase().includes(search.toLowerCase()));
 useEffect(() => { setCurrent(db); }, [db.revision]);
 useEffect(() => () => { sequence.current++; }, []);
 useEffect(() => { if (route.selection !== selected?.id) selectResource(selected?.id, true); }, [route.selection, selected?.id]);
 useEffect(() => { setData(initialData); setApplied(undefined); setError(''); setRender(0); }, [selected?.id]);
 const reload = async () => {
  const request = ++sequence.current; setLoading(true); setError('');
  try { const next = await databases.get(db.id); if (request === sequence.current) { setCurrent(next); onSaved(); } }
  catch(e) { if (request === sequence.current) setError((e as Error).message); }
  finally { if (request === sequence.current) setLoading(false); }
 };
 const preview = () => {
  try {
   const value = JSON.parse(data);
   if (!value || typeof value !== 'object' || Array.isArray(value) || value.input !== undefined && (!value.input || typeof value.input !== 'object' || Array.isArray(value.input))) throw new Error(t('视图数据必须是 JSON 对象，input 也必须是对象。'));
   setApplied({ input: value.input, result: value.result }); setError(''); setRender(old => old + 1); setDetailView('preview');
  } catch(e) { setError((e as Error).message); }
 };
 return <div className="action-workspace">
  <Card className="action-list-panel" title={<Space>{t('视图列表')}<Tag>{items.length}</Tag></Space>} extra={<IconButton label={t('刷新')} icon={<ReloadOutlined />} loading={loading} onClick={() => void reload()} />}>
   <Input aria-label={t('搜索视图')} placeholder={t('搜索名称、描述或标识')} value={search} onChange={event => setSearch(event.target.value)} allowClear />
   <ul className="action-list" aria-label={t('视图列表')}>{visible.map(v => <li key={v.id}><button type="button" className={`action-list-item view-list-item${selected?.id === v.id ? ' selected' : ''}`} aria-current={selected?.id === v.id ? 'true' : undefined} onClick={() => selectResource(v.id)}>
    <strong title={v.name}>{v.name}</strong><span className="view-entry-id" title={v.id}>{v.id}</span><Tag className="view-entry-status" color={v.published ? 'green' : 'default'}>{t(v.published ? '已发布' : '草稿')}</Tag>
   </button></li>)}</ul>
   {!visible.length && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('暂无匹配视图')} />}
  </Card>
  <Card className="action-editor-panel" title={<Space>{selected?.name || t('选择视图')}<HelpTip label={t('模拟数据预览')}>{t('使用已保存的 HTML 和模拟数据预览，支持草稿。业务工具调用请切换到真实业务预览并完成本人认证。')}</HelpTip></Space>}>
   {error && <Alert type="error" showIcon title={error} style={{ marginBottom: 12 }} />}
   {selected ? <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
    {selected.description && <Typography.Paragraph type="secondary">{selected.description}</Typography.Paragraph>}
    <div style={{ width: '100%' }}>
     <div className="content-field-header"><Space size={4}>{t('视图数据 JSON')}<HelpTip label={t('视图数据 JSON')}>{t('input 为视图输入，result 为模拟的查询或动作结果。数据仅保留在当前页面，不会保存到视图配置。')}</HelpTip></Space><IconButton label={t('渲染预览')} icon={<PlayCircleOutlined />} type="primary" onClick={preview} /></div>
     <CodeEditor language="json" height="160px" value={data} onChange={setData} />
    </div>
    <Tabs style={{ width: '100%' }} activeKey={detailView} onChange={setDetailView} destroyOnHidden items={[
     { key: 'preview', label: t('预览'), children: <ViewFrame key={`${selected.id}:${render}`} html={selected.html} data={applied} /> },
     { key: 'html', label: 'HTML', children: <CodeEditor language="html" height="520px" value={selected.html} readOnly /> },
    ]} />
   </Space> : <Empty description={t('从左侧选择视图，或前往视图配置创建。')} />}
  </Card>
 </div>;
}

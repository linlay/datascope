import { lazy, Suspense, useEffect, useRef, useState } from 'react';
import { Button, Card, Empty, Input, Space, Spin, Tabs, Tag, Typography } from 'antd';
import type { KnowledgeEntry, KnowledgeSearch, Knowledge } from '../api';
import { HelpTip } from '../components/HelpTip';
import { CallDetails, JsonData, invoke, type Invocation } from '../components/ToolCallTrace';
import { useI18n } from '../ui/preferences';
import { selectResource, useRoute, useRouteChoice } from '../navigation';

const KnowledgeContent = lazy(() => import('../components/KnowledgeContent'));
export default function KnowledgeWorkbench({ databaseID }: { databaseID: string }) {
 // A database change starts a fresh workflow, discarding pending responses.
 return <KnowledgeWorkflow key={databaseID} databaseID={databaseID} />;
}

function KnowledgeWorkflow({ databaseID }: { databaseID: string }) {
 const t = useI18n();
 const route=useRoute();
 const [query, setQuery] = useState('');
 const [knowledgeID, setKnowledgeID] = useState('');
 const [searchResult, setSearchResult] = useState<Invocation<KnowledgeSearch>>();
 const [entries,setEntries]=useState<KnowledgeEntry[]>([]);
 const [readResult, setReadResult] = useState<Invocation<Knowledge>>();
 const [searching, setSearching] = useState(false);
 const [reading, setReading] = useState(false);
 const searchedKeyword=useRef('');
 const [searchTab, setSearchTab] = useRouteChoice('listView','list',['list','json']);
 const [readTab, setReadTab] = useRouteChoice('detailView','json',['json','preview']);
 const selectedRequest=useRef('');
 const searchSequence = useRef(0);
 const readSequence = useRef(0);
 useEffect(() => () => { selectedRequest.current=''; searchSequence.current++; readSequence.current++; }, []);

 const selectID = (id: string, replace=false) => {
  readSequence.current++;
  selectedRequest.current=id;
  setKnowledgeID(id); setReadResult(undefined); setReading(false);selectResource(id||undefined,replace);setReadTab('json',true);
 };
 const search = async (cursor?:string,clear=true) => {
  const request = ++searchSequence.current;
  if(!cursor){if(clear)selectID('');setEntries([])} setSearching(true); setSearchResult(undefined); if(clear)setSearchTab('list',true);
  if (!cursor) searchedKeyword.current=query.trim();
  const result = await invoke<KnowledgeSearch>(databaseID, 'search_knowledge', {keyword:searchedKeyword.current,...(cursor?{cursor}:{})});
  if (request !== searchSequence.current) return;
  setSearchResult(result); if(result.data)setEntries(previous=>cursor?[...previous,...result.data!.items]:result.data!.items); setSearching(false);
 };
 const read = async (id=knowledgeID.trim()) => {
  if (!id) return;
  const request = ++readSequence.current;
  setReading(true); setReadResult(undefined);
  const result = await invoke<Knowledge>(databaseID, 'read_knowledge', { id });
  if (request !== readSequence.current) return;
  setReadResult(result); setReading(false);
 };
 useEffect(()=>{
  const name=route.selection||'';
  if(name===selectedRequest.current)return;
  selectedRequest.current=name;readSequence.current++;setKnowledgeID(name);setReadResult(undefined);setReading(false);
  if(name){void search(undefined,false);void read(name);}
 },[route.selection]);
 const items = searchResult?.data ? entries : undefined;
 const document = readResult?.data;

 return <div className="knowledge-call-workspace">
   <Card size="small" className="knowledge-call-panel knowledge-search-step" title={<Space size={4}>{t('1. 查询知识列表')}<HelpTip label={t('模拟身份')}>{t('复用业务 MCP 的知识检索逻辑，使用当前平台账号权限。业务账号的实际可见范围需通过业务 MCP 登录验证。')}</HelpTip></Space>} extra={<Typography.Text code>search_knowledge</Typography.Text>}>
    <form className="knowledge-call-form" onSubmit={event => { event.preventDefault(); void search(); }}>
     <Input aria-label={t('查询知识')} placeholder={t('输入关键词，留空浏览全部已发布知识')} value={query} allowClear onChange={event => setQuery(event.target.value)} />
     <Button type="primary" htmlType="submit" loading={searching}>{t('查询列表')}</Button>
    </form>
    {searchResult && <CallDetails invocation={searchResult} />}
    <Spin spinning={searching}>
     {items ? <Tabs size="small" activeKey={searchTab} onChange={setSearchTab} items={[
      { key: 'list', label: <Space size="small">{t('知识列表')}<Tag>{items.length}</Tag></Space>, children: <>
       <ul className="knowledge-call-results">
        {items.map(item => <li key={item.id} className={knowledgeID === item.id ? 'selected' : undefined}>
         <div className="knowledge-call-entry-heading"><strong>{item.name}</strong><Button size="small" onClick={() => selectID(item.id)}>{t('使用此 ID')}</Button></div>
         <Typography.Text className="mono" type="secondary" copyable={{ text: item.id }}>{item.id}</Typography.Text>
         <p className="knowledge-call-description">{item.description}</p>
        </li>)}
       </ul>
       {!items.length && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('没有匹配的已发布知识')} />}
       {searchResult?.data?.next_cursor && <Button loading={searching} onClick={()=>void search(searchResult.data!.next_cursor)}>{t('加载下一页')}</Button>}
      </> },
      { key: 'json', label: t('返回 JSON'), children: <JsonData value={searchResult?.data} /> },
     ]} /> : !searchResult?.error && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('点击查询列表，查看模型先收到的名称、ID 和 description。')} />}
    </Spin>
   </Card>
   <Card size="small" className="knowledge-call-panel knowledge-read-step" title={t('2. 按 ID 读取正文')} extra={<Typography.Text code>read_knowledge</Typography.Text>}>
    <form className="knowledge-call-form" onSubmit={event => { event.preventDefault(); void read(); }}>
     <Input aria-label={t('知识 ID')} placeholder={t('从列表选择或输入知识 ID')} value={knowledgeID} disabled={!items || searching} allowClear onChange={event => selectID(event.target.value,true)} />
     <Button type="primary" htmlType="submit" disabled={!items || searching || !knowledgeID.trim()} loading={reading}>{t('读取正文')}</Button>
    </form>
    {readResult && <CallDetails invocation={readResult} />}
    <Spin spinning={reading}>
     {document ? <Tabs size="small" activeKey={readTab} onChange={setReadTab} items={[
      { key: 'json', label: t('返回 JSON'), children: <JsonData value={document} /> },
      { key: 'preview', label: t('正文预览'), children: <><Typography.Title level={5}>{document.name}</Typography.Title><Suspense fallback={<Spin />}><KnowledgeContent content={document.content || ''} /></Suspense></> },
     ]} /> : !readResult?.error && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('先查询列表，再选择 ID 并点击读取正文。')} />}
    </Spin>
   </Card>
 </div>;
}

import { useEffect, useState } from 'react';
import { Alert, Button, Card, Checkbox, Col, Descriptions, Input, Modal, Row, Select, Space, Statistic, Switch, Table, Tag, Typography, message } from 'antd';
import { databases, type Annotation, type Column, type Database, type SchemaChange, type TableSchema } from '../api';
import { useAuth } from '../Auth';

const { Text, Paragraph } = Typography;
const kinds = { added: '新增', removed: '删除', modified: '修改' };
const properties: Record<string,string> = { table:'表',column:'字段',comment:'注释',type:'类型',not_null:'非空',primary_key:'主键',default:'默认值',references:'外键',indexes:'索引',ddl:'结构定义' };
const date = (v?:string) => v ? new Date(v).toLocaleString('zh-CN') : '尚未侦查';

export function DiscoveryPage({ id, onSaved }: {id:string;onSaved:()=>void}) {
 const { user } = useAuth();
 const [db,setDB] = useState<Database>();
 const [active,setActive] = useState<string>();
 const [search,setSearch] = useState('');
 const [error,setError] = useState<string>();
 const [busy,setBusy] = useState(false);
 const [dirty,setDirty] = useState(false);
 const [report,setReport] = useState<number>();
 const [editor,setEditor] = useState<{table:string;column:string;source:string;annotation:Annotation}>();
 const accept = (d:Database) => {setDB(d);setDirty(false);setActive(prev=>d.discovery?.tables.some(t=>t.name===prev)?prev:d.discovery?.tables[0]?.name);};
 useEffect(()=>{let live=true;databases.get(id).then(d=>{if(live)accept(d)}).catch(e=>{if(live)setError(e.message)});return()=>{live=false}},[id]);
 const run = async (work:()=>Promise<Database>,text:string) => {setBusy(true);setError(undefined);try {accept(await work());message.success(text);onSaved();return true;}catch(e){setError((e as Error).message);return false;}finally{setBusy(false)}};
 if(!db)return <Card loading={!error}>{error&&<Alert type="error" title={error}/>}</Card>;
 const can = (p:string) => user.system_admin || db.permissions?.includes(p);
 const state = db.discovery;
 const tables = state?.tables ?? [];
 const current = tables.find(t=>t.name===active);
 const annotation = state?.annotations?.[active??''];
 const policy = db.config.tables.find(p=>p.table.toLowerCase()===active?.toLowerCase());
 const reports = state?.reports??[];
 const selectedReport = reports.find(r=>r.version===report)??reports[reports.length-1];
 const setPolicy = (table:string,patch:Record<string,unknown>) => {setDB({...db,config:{...db.config,tables:db.config.tables.map(p=>p.table.toLowerCase()===table.toLowerCase()?{...p,...patch}:p)}});setDirty(true)};
 const editAnnotation = (table:string,column:string,source:string,a?:Annotation) => setEditor({table,column,source,annotation:a?{...a}:{display_name:'',comment:null}});
 return <Space className="discovery-page" orientation="vertical" size="middle" style={{width:'100%'}}>
  <Card className="discovery-overview" title="数据库侦查与映射" extra={<Space wrap>{dirty&&<Tag color="orange">开放范围有未保存修改</Tag>}<Button disabled={busy} onClick={()=>void run(()=>databases.get(id),'已读取最新配置')}>读取最新配置</Button><Button loading={busy} disabled={dirty||!can('config.write')} onClick={()=>void run(()=>databases.discover(id),'侦查完成，已采用最新结构；人工说明已保留')}>立即侦查变化</Button><Button type="primary" loading={busy} disabled={!dirty||!can('policy.write')} onClick={()=>void run(()=>databases.save(db),'开放范围已保存并生效')}>保存开放范围</Button></Space>}>
   <Paragraph type="secondary">接入时自动读取全部表、字段、注释、索引和外键。名称映射与人工说明独立保存，更新结构时保留；没有注释的表和字段可直接补充。数据内容通过「浏览数据」分页读取。</Paragraph>
   <Row gutter={[32,12]}><Col><Statistic title="发现数据表" value={tables.length}/></Col><Col><Statistic title="发现字段" value={tables.reduce((n,t)=>n+t.columns.length,0)}/></Col><Col><Statistic title="结构版本" value={state?.version??0}/></Col><Col><Text type="secondary">最近成功侦查</Text><div>{date(state?.checked_at)}</div></Col></Row>
   {!state?.version&&<Alert style={{marginTop:12}} type="info" title="尚无完整结构快照，点击「立即侦查变化」自动建立。"/>}
   {state?.error&&<Alert style={{marginTop:12}} type="error" title="自动侦查未成功，已保留上次完整快照" description={state.error}/>}
  </Card>
  {error&&<Alert type="error" showIcon title="操作失败" description={error} closable onClose={()=>setError(undefined)}/>}
  <Row gutter={[16,16]}>
   <Col xs={24} xl={9}><Card title="全部数据表" extra={<Input aria-label="搜索侦查结果" placeholder="表名、映射或说明" value={search} onChange={e=>setSearch(e.target.value)} allowClear style={{width:170}}/>}>
    <Table<TableSchema> rowKey="name" size="small" dataSource={tables.filter(t=>[t.name,t.comment,state?.annotations?.[t.name]?.display_name,state?.annotations?.[t.name]?.comment].join(' ').toLowerCase().includes(search.toLowerCase()))} pagination={{pageSize:12}} scroll={{x:400}} onRow={t=>({onClick:()=>setActive(t.name),style:{cursor:'pointer',background:t.name===active?'rgba(0,113,227,.08)':undefined}})} columns={[
     {title:'物理表 / 映射',render:(_,t)=><><Text code>{t.name}</Text><div>{state?.annotations?.[t.name]?.display_name||t.comment||<Text type="secondary">待补充说明</Text>}</div></>},
     {title:'字段',render:(_,t)=>t.columns.length,width:55},
     {title:'开放',width:70,render:(_,t)=><Switch size="small" aria-label={`开放表 ${t.name}`} disabled={busy||!can('policy.write')} checked={db.config.tables.find(p=>p.table.toLowerCase()===t.name.toLowerCase())?.enabled!==false} onClick={(_,e)=>e.stopPropagation()} onChange={enabled=>setPolicy(t.name,{enabled})}/>},
    ]}/>
   </Card></Col>
   <Col xs={24} xl={15}><Card title={current?`表与字段 · ${current.name}`:'选择数据表'} extra={current&&<Space><Button disabled={busy||dirty||!can('config.write')} onClick={()=>editAnnotation(current.name,'',current.comment,annotation)}>编辑表映射</Button>{can('raw_query')&&<Button onClick={()=>{window.location.hash=`#/table/${encodeURIComponent(current.name)}`}}>浏览数据</Button>}</Space>}>
    {current&&<>
     <Descriptions size="small" column={1} items={[
      {key:'name',label:'映射名称',children:annotation?.display_name||current.name},
      {key:'source',label:'数据库原始注释',children:current.comment||'（空）'},
      {key:'effective',label:'当前说明',children:<Space><Text>{annotation?.comment??current.comment??''}</Text>{annotation?.comment!=null?<Tag color="blue">人工覆盖</Tag>:<Tag>跟随数据库</Tag>}</Space>},
     ]}/>
     <Space style={{margin:'12px 0'}}><Checkbox disabled={busy||!can('policy.write')||!policy} checked={policy?.columns==null} onChange={e=>setPolicy(current.name,{columns:e.target.checked?null:current.columns.map(c=>c.name)})}>包含未来新增字段</Checkbox><Button size="small" disabled={busy||!can('policy.write')} onClick={()=>setPolicy(current.name,{columns:current.columns.map(c=>c.name)})}>选择当前全部字段</Button></Space>
     <Table<Column> rowKey="name" size="small" dataSource={current.columns} pagination={{pageSize:10}} scroll={{x:700}} columns={[
      {title:'开放',width:60,render:(_,c)=><Checkbox aria-label={`开放字段 ${c.name}`} disabled={busy||!can('policy.write')} checked={policy?.columns==null||policy.columns.includes(c.name)} onChange={e=>{const cols=policy?.columns??current.columns.map(col=>col.name);setPolicy(current.name,{columns:e.target.checked?[...cols,c.name]:cols.filter(n=>n!==c.name)})}}/>},
      {title:'字段 / 类型',render:(_,c)=><><Text code>{c.name}</Text>{c.primary_key&&<Tag color="gold">主键</Tag>}<div><Text type="secondary">{c.type}{c.not_null?' · 非空':''}</Text></div></>},
      {title:'映射名称',render:(_,c)=>annotation?.columns?.[c.name]?.display_name||c.name},
      {title:'数据库注释',render:(_,c)=>c.comment||<Text type="secondary">（空）</Text>},
      {title:'当前说明',render:(_,c)=>{const a=annotation?.columns?.[c.name];return <>{a?.comment??c.comment}{a?.comment!=null&&<Tag color="blue">人工</Tag>}</>}},
      {title:'操作',width:70,render:(_,c)=><Button type="link" size="small" disabled={busy||dirty||!can('config.write')} onClick={()=>editAnnotation(current.name,c.name,c.comment,annotation?.columns?.[c.name])}>编辑</Button>},
     ]}/>
    </>}
   </Card></Col>
  </Row>
  <Card title="结构变更对比" extra={<Select aria-label="选择变更版本" style={{width:270}} value={selectedReport?.version} onChange={setReport} options={[...reports].reverse().map(r=>({value:r.version,label:`版本 ${r.version} · ${date(r.at)}`}))}/> }>
   <Paragraph type="secondary">采用最新数据库结构，保留人工映射与说明。新增表默认禁止；字段改名按删除和新增处理。人工说明与新的数据库注释不一致时标记冲突，可在上方编辑中切换为跟随数据库。最近保留 20 次结构变更报告。</Paragraph>
   <Table<SchemaChange> rowKey={(_,i)=>String(i)} size="small" dataSource={selectedReport?.changes??[]} pagination={{pageSize:10}} scroll={{x:900}} columns={[
    {title:'对象',render:(_,c)=><Text code>{c.table}{c.column?`.${c.column}`:''}</Text>},
    {title:'变化',render:(_,c)=><Space><Tag color={c.kind==='removed'?'red':c.kind==='added'?'green':'blue'}>{kinds[c.kind]}</Tag>{properties[c.property]||c.property}</Space>},
    {title:'更新前',dataIndex:'before',ellipsis:true,width:180},
    {title:'数据库新值',dataIndex:'after',ellipsis:true,width:180},
    {title:'人工值 / 处理',render:(_,c)=>c.manual!==undefined?<><div>{c.manual||'（空）'}</div><Tag color={c.conflict?'orange':'blue'}>{c.conflict?'冲突 · 保留人工值':'保留人工值'}</Tag></>:c.kind==='added'?<Tag>待选择开放</Tag>:<Tag>采用数据库新值</Tag>},
   ]}/>
  </Card>
  <Modal title={`编辑映射与说明 · ${editor?.table??''}${editor?.column?`.${editor.column}`:''}`} open={!!editor} confirmLoading={busy} onCancel={()=>setEditor(undefined)} onOk={async()=>{if(!editor)return;const value=editor;if(await run(()=>databases.annotate(id,value.table,value.column,db.revision,value.annotation),'映射与说明已保存'))setEditor(undefined)}} okText="保存映射">
   {editor&&<Space orientation="vertical" style={{width:'100%'}}>
    <Text type="secondary">名称映射只影响展示，SQL 始终使用物理表名和字段名。</Text>
    <Text>映射名称</Text><Input aria-label="映射名称" maxLength={170} value={editor.annotation.display_name} onChange={e=>setEditor({...editor,annotation:{...editor.annotation,display_name:e.target.value}})}/>
    <Text>数据库原始注释：{editor.source||'（空）'}</Text>
    <Checkbox checked={editor.annotation.comment===null} onChange={e=>setEditor({...editor,annotation:{...editor.annotation,comment:e.target.checked?null:editor.source}})}>跟随数据库注释（取消人工覆盖）</Checkbox>
    <Input.TextArea aria-label="人工说明" rows={5} disabled={editor.annotation.comment===null} value={editor.annotation.comment??editor.source} onChange={e=>setEditor({...editor,annotation:{...editor.annotation,comment:e.target.value}})}/>
   </Space>}
  </Modal>
 </Space>;
}

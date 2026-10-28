import { useI18n } from '../ui/preferences';
import { useEffect, useState } from 'react';
import { Alert, Button, Card, Checkbox, Col, Collapse, Descriptions, Input, Modal, Row, Select, Space, Statistic, Switch, Table, Tag, Tooltip, Typography, message } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { databases, type Annotation, type Column, type Database, type PolicyView, type SchemaChange, type TableSchema } from '../api';
import { useAuth } from '../Auth';
import { TablePolicyEditor } from './DatabaseManager';
import { setAllTableAccess } from '../tableAccess';

const { Text, Paragraph } = Typography;
const kinds = { added: '新增', removed: '删除', modified: '修改' };
const properties: Record<string,string> = { table:'表',column:'字段',comment:'注释',type:'类型',not_null:'非空',primary_key:'主键',default:'默认值',references:'外键',indexes:'索引',ddl:'结构定义' };
const date = (value?:string) => value && !value.startsWith('0001-') ? new Date(value).toLocaleString() : '';
const changeKey = (change:SchemaChange) => JSON.stringify([change.table,change.column??'']);

export function DiscoveryPage({ id, onSaved }: {id:string;onSaved:()=>void}) {
 const t=useI18n();
 const { user } = useAuth();
 const [db,setDB] = useState<Database>();
 const [active,setActive] = useState<string>();
 const [search,setSearch] = useState('');
 const [error,setError] = useState<string>();
 const [busy,setBusy] = useState(false);
 const [dirty,setDirty] = useState(false);
 const [remoteChanged,setRemoteChanged] = useState(false);
 const [fragments,setFragments] = useState('{}');
 const [report,setReport] = useState<number>();
 const [comparison,setComparison] = useState<'pending'|'history'>();
 const [choices,setChoices] = useState<Record<string,boolean>>({});
 const [editor,setEditor] = useState<{table:string;column:string;source:string;annotation:Annotation}>();
 const accept = (d:Database) => {
  setDB(d);setDirty(false);setRemoteChanged(false);setFragments(JSON.stringify(d.config.fragments??{},null,2));
  setActive(previous=>d.discovery?.tables?.some(tableInfo=>tableInfo.name===previous)?previous:d.discovery?.tables?.[0]?.name);
 };
 useEffect(()=>{let live=true;databases.get(id).then(d=>{if(live)accept(d)}).catch(e=>{if(live)setError(e.message)});return()=>{live=false}},[id]);
 // Background detection can surface a proposal while an administrator is on
 // this screen. Never replace a draft or an annotation being edited.
 useEffect(()=>{
  let live=true;
  const timer=window.setInterval(()=>{
   if(busy||editor)return;
   void databases.get(id).then(next=>{
    if(!live)return;
    setDB(previous=>{
     if(!previous)return next;
     if(dirty){if(next.revision!==previous.revision)setRemoteChanged(true);return {...previous,discovery:previous.discovery?{...previous.discovery,pending:next.discovery?.pending,checked_at:next.discovery?.checked_at??previous.discovery.checked_at,error:next.discovery?.error}:previous.discovery}}
     return next;
    });
   }).catch(()=>undefined);
  },15000);
  return()=>{live=false;window.clearInterval(timer)};
 },[id,dirty,busy,editor]);
 const run = async (work:()=>Promise<Database>,text:string|((d:Database)=>string)) => {
  setBusy(true);setError(undefined);
  try {const next=await work();accept(next);message.success(typeof text==='string'?text:text(next));onSaved();return true}
  catch(e){setError((e as Error).message);return false}
  finally{setBusy(false)}
 };
 if(!db)return <Card loading={!error}>{error&&<Alert type="error" title={error}/>}</Card>;
 const can = (p:string) => user.system_admin || !!db.permissions?.includes(p);
 const state = db.discovery;
 const tables = state?.tables ?? [];
 const visibleTables=tables.filter(table=>[table.name,table.comment,state?.annotations?.[table.name]?.display_name,state?.annotations?.[table.name]?.comment].join(' ').toLowerCase().includes(search.toLowerCase()));
 const current = tables.find(tableInfo=>tableInfo.name===active);
 const annotation = state?.annotations?.[active??''];
 const policy = db.config.tables.find(p=>p.table.toLowerCase()===active?.toLowerCase());
 const reports = state?.reports??[];
 const selectedReport = reports.find(r=>r.version===report)??reports[reports.length-1];
 const pending=state?.pending;
 const changes=comparison==='pending'?(pending?.changes??[]):selectedReport?.changes??[];
 const setPolicy = (table:string,patch:Partial<PolicyView>) => {
  if(patch.columns?.length===0)patch={...patch,enabled:false};
  setDB({...db,config:{...db.config,tables:db.config.tables.map(p=>p.table.toLowerCase()===table.toLowerCase()?{...p,...patch}:p)}});setDirty(true);
 };
 const setAllEnabled=(enabled:boolean)=>{
  setDB({...db,config:{...db.config,tables:setAllTableAccess(db.config.tables,tables,enabled)}});
  setDirty(true);
 };
 const editAnnotation = (table:string,column:string,source:string,a?:Annotation) => setEditor({table,column,source,annotation:a?{...a}:{display_name:'',comment:null}});
 const scan=()=>run(async()=>{const next=await databases.discover(id);if(next.discovery?.pending){setChoices({});setComparison('pending')}return next},next=>next.discovery?.pending?t("发现结构变化，请对比并确认"):t("检查完成，数据库结构一致"));
 const save=()=>run(async()=>{
  const parsed=JSON.parse(fragments);
  if(!parsed||Array.isArray(parsed)||typeof parsed!=='object'||Object.values(parsed).some(v=>typeof v!=='string'))throw new Error(t("共享 SQL 片段必须是名称到 SQL 字符串的 JSON 对象"));
  return databases.save({...db,config:{...db.config,fragments:parsed}});
 },t("表配置与权限已保存"));
 const diffColumns:ColumnsType<SchemaChange>=[
  {title:t("表 / 字段"),render:(_,change)=><Text code>{change.table}{change.column?`.${change.column}`:''}</Text>,width:200},
  {title:t("变化"),render:(_,change)=><Space><Tag color={change.kind==='removed'?'red':change.kind==='added'?'green':'blue'}>{t(kinds[change.kind])}</Tag>{t(properties[change.property]||change.property)}</Space>,width:130},
  {title:t("原数据库内容"),dataIndex:'before',ellipsis:true,width:200},
  {title:t("当前数据库内容"),dataIndex:'after',ellipsis:true,width:200},
  {title:t("人工说明 / 处理"),width:220,render:(_,change)=>change.manual!==undefined?<Space orientation="vertical"><Text>{change.manual||'（空）'}</Text>{change.conflict&&<Tag color="orange">{t("与人工说明存在差异")}</Tag>}{comparison==='pending'?<Select aria-label={t("处理注释 {0}{1}", {"0":(change.table),"1":(change.column?`.${change.column}`:'')})} style={{width:180}} value={choices[changeKey(change)]?'source':'manual'} onChange={value=>setChoices({...choices,[changeKey(change)]:value==='source'})} options={[{value:'manual',label:t("保留人工说明")},{value:'source',label:t("采用数据库新注释")}]}/>:<Tag>{t("已确认的结构变化")}</Tag>}</Space>:<Text type="secondary">{change.kind==='added'?t("确认后登记，默认关闭"):t("确认后更新结构")}</Text>},
 ];
 return <Space className="discovery-page" orientation="vertical" size="middle" style={{width:'100%'}}>
  <Card className="discovery-overview" title={t("表配置与权限")} extra={<Space wrap>
   {dirty&&<Tag color="orange">{t("有未保存修改")}</Tag>}
   <Button disabled={busy} onClick={()=>void run(()=>databases.get(id),t("已读取最新配置"))}>{t("读取最新配置")}</Button>
   <Button disabled={busy||reports.length===0} onClick={()=>setComparison('history')}>{t("变更记录")}</Button>
   <Button loading={busy} disabled={dirty||!can('config.write')} onClick={()=>void scan()}>{t("检查数据库变化")}</Button>
   <Button type="primary" loading={busy} disabled={!dirty||!can('policy.write')} onClick={()=>void save()}>{t("保存表配置")}</Button>
  </Space>}>
   <Paragraph type="secondary">{t("统一配置表和字段的开放范围、名称、说明以及行权限。数据库结构变化先对比，确认后再更新配置。")}</Paragraph>
   <Row gutter={[32,12]}><Col><Statistic title={t("已登记表")} value={tables.length}/></Col><Col><Statistic title={t("已开放表")} value={db.config.tables.filter(p=>p.enabled!==false).length}/></Col><Col><Statistic title={t("已登记字段")} value={tables.reduce((n,tableInfo)=>n+tableInfo.columns.length,0)}/></Col><Col><Text type="secondary">{t("最近结构检查")}</Text><div>{date(state?.checked_at)||t('尚未检查')}</div></Col></Row>
   {!state?.version&&<Alert style={{marginTop:12}} type="info" title={t("点击「检查数据库变化」建立首次表结构。")}/>}
   {state?.error&&<Alert style={{marginTop:12}} type="error" title={t("自动检查未成功，已保留原配置")} description={state.error}/>}
  </Card>
  {pending&&<Alert type="warning" showIcon title={t("数据库结构与当前配置不一致，发现 {0} 项变化", {"0":(pending.changes.length)})} description={t("当前配置保持不变，请先查看对比，再确认更新。")} action={<Button disabled={busy} onClick={()=>{setChoices({});setComparison('pending')}}>{t("查看对比")}</Button>}/>}
  {remoteChanged&&<Alert type="warning" title={t("配置已在其他窗口更新，请读取最新配置后继续。")}/>}
  {error&&<Alert type="error" showIcon title={t("操作失败")} description={error} closable onClose={()=>setError(undefined)}/>}
  <Row gutter={[16,16]}>
   <Col xs={24} xl={8}><Card title={t("数据表")} extra={<Input aria-label={t("搜索数据表")} placeholder={t("表名、名称或说明")} value={search} onChange={e=>setSearch(e.target.value)} allowClear style={{width:170}}/>}>
    <Space wrap style={{marginBottom:12}}>
     <Tooltip title={t('作用于当前数据库的全部已登记表，保存后生效。')}><Button disabled={busy||!can('policy.write')||tables.length===0} onClick={()=>setAllEnabled(true)}>{t('全部开放')}</Button></Tooltip>
     <Tooltip title={t('作用于当前数据库的全部已登记表，保存后生效。')}><Button disabled={busy||!can('policy.write')||tables.length===0} onClick={()=>setAllEnabled(false)}>{t('全部关闭')}</Button></Tooltip>
     <Text type="secondary">{t('显示 {visible} / {total} 张表',{visible:visibleTables.length,total:tables.length})}</Text>
    </Space>
    <Table<TableSchema> rowKey="name" size="small" dataSource={visibleTables} pagination={false} scroll={{x:330,y:'min(60vh, 600px)'}} onRow={tableInfo=>({onClick:()=>setActive(tableInfo.name),style:{cursor:'pointer',background:tableInfo.name===active?'rgba(0,113,227,.08)':undefined}})} columns={[
     {title:t("数据表 / 名称"),render:(_,tableInfo)=><><Text code>{tableInfo.name}</Text><div>{state?.annotations?.[tableInfo.name]?.display_name||tableInfo.comment||<Text type="secondary">{t("待补充说明")}</Text>}</div></>},
     {title:t("字段"),render:(_,tableInfo)=>tableInfo.columns.length,width:55},
     {title:t("开放"),width:70,render:(_,tableInfo)=><Switch size="small" aria-label={t("开放表 {0}", {"0":(tableInfo.name)})} disabled={busy||!can('policy.write')} checked={db.config.tables.find(p=>p.table.toLowerCase()===tableInfo.name.toLowerCase())?.enabled!==false} onClick={(_,e)=>e.stopPropagation()} onChange={enabled=>setPolicy(tableInfo.name,{enabled})}/>},
    ]}/>
   </Card></Col>
   <Col xs={24} xl={16}><Card title={current?t("表与字段 · {0}", {"0":(current.name)}):t("选择数据表")} extra={current&&<Space><Button disabled={busy||dirty||!can('config.write')} onClick={()=>editAnnotation(current.name,'',current.comment,annotation)}>{t("编辑表说明")}</Button>{can('raw_query')&&<Button onClick={()=>{window.location.hash=`#/table/${encodeURIComponent(current.name)}`}}>{t("浏览数据")}</Button>}</Space>}>
    {current&&<Space orientation="vertical" size="middle" style={{width:'100%'}}>
     <Descriptions size="small" column={1} items={[
      {key:'name',label:t("显示名称"),children:annotation?.display_name||current.name},
      {key:'source',label:t("数据库注释"),children:current.comment||'（空）'},
      {key:'effective',label:t("当前说明"),children:<Space><Text>{annotation?.comment??current.comment??''}</Text>{annotation?.comment!=null?<Tag color="blue">{t("人工维护")}</Tag>:<Tag>{t("跟随数据库")}</Tag>}</Space>},
     ]}/>
     <Space wrap><Checkbox disabled={busy||!can('policy.write')||!policy} checked={policy?.columns==null} onChange={e=>setPolicy(current.name,{columns:e.target.checked?null:current.columns.map(c=>c.name)})}>{t("包含未来新增字段")}</Checkbox><Button size="small" disabled={busy||!can('policy.write')} onClick={()=>setPolicy(current.name,{columns:current.columns.map(c=>c.name)})}>{t("选择当前全部字段")}</Button></Space>
     <Table<Column> key={current.name} rowKey="name" size="small" dataSource={current.columns} pagination={false} scroll={{x:700,y:'min(52vh, 560px)'}} columns={[
      {title:t("开放"),width:60,render:(_,c)=><Checkbox aria-label={t("开放字段 {0}", {"0":(c.name)})} disabled={busy||!can('policy.write')} checked={policy?.columns==null||policy.columns.includes(c.name)} onChange={e=>{const cols=policy?.columns??current.columns.map(col=>col.name);setPolicy(current.name,{columns:e.target.checked?[...cols,c.name]:cols.filter(n=>n!==c.name)})}}/>},
      {title:t("字段 / 类型"),render:(_,c)=><><Text code>{c.name}</Text>{c.primary_key&&<Tag color="gold">{t("主键")}</Tag>}<div><Text type="secondary">{c.type}{c.not_null?t(" · 非空"):''}</Text></div></>},
      {title:t("显示名称"),render:(_,c)=>annotation?.columns?.[c.name]?.display_name||c.name},
      {title:t("数据库注释"),render:(_,c)=>c.comment||<Text type="secondary">{t("（空）")}</Text>},
      {title:t("当前说明"),render:(_,c)=>{const a=annotation?.columns?.[c.name];return <>{a?.comment??c.comment}{a?.comment!=null&&<Tag color="blue">{t("人工")}</Tag>}</>}},
      {title:t("操作"),width:70,render:(_,c)=><Button type="link" size="small" disabled={busy||dirty||!can('config.write')} onClick={()=>editAnnotation(current.name,c.name,c.comment,annotation?.columns?.[c.name])}>{t("编辑")}</Button>},
     ]}/>
     {policy&&<Collapse items={[{key:'row',label:t("行权限与查询限制"),children:<TablePolicyEditor db={db} policy={policy} disabled={busy||!can('policy.write')} onChange={patch=>setPolicy(current.name,patch)}/>} ]}/>}
    </Space>}
   </Card></Col>
  </Row>
  <Collapse items={[{key:'fragments',label:t("共享 SQL 片段"),children:<Input.TextArea aria-label={t("共享 SQL 片段")} className="mono" rows={6} disabled={busy||!can('policy.write')} value={fragments} onChange={e=>{setFragments(e.target.value);setDirty(true)}}/>}]}/>
  <Modal width={1120} title={comparison==='pending'?t("数据库结构变化对比"):t("结构变更记录")} open={!!comparison} onCancel={()=>setComparison(undefined)} okText={t("确认并更新配置")} confirmLoading={busy} okButtonProps={{disabled:dirty||!pending||!can('config.write')||!can('policy.write')}} onOk={async()=>{
   if(!pending)return;
   const resolutions=pending.changes.filter(c=>c.property==='comment'&&c.manual!==undefined).map(c=>({table:c.table,column:c.column,follow_source:choices[changeKey(c)]??false}));
   if(await run(()=>databases.applyDiscovery(id,pending.id,db.revision,resolutions),t("已按确认结果更新表结构与权限配置")))setComparison(undefined);
  }} {...(comparison==='history'?{footer:<Button onClick={()=>setComparison(undefined)}>{t("关闭")}</Button>}: {})}>
   {comparison==='pending'?<Alert type="info" showIcon title={t("确认后采用新的表结构，新增表默认关闭，已有字段权限不会自动扩大。人工说明默认保留，也可以逐项选择采用数据库新注释。")} style={{marginBottom:16}}/>:<Select aria-label={t("选择变更版本")} style={{width:300,marginBottom:16}} value={selectedReport?.version} onChange={setReport} options={[...reports].reverse().map(r=>({value:r.version,label:t("版本 {0} · {1}", {"0":(r.version),"1":(date(r.at))})}))}/>}
   {dirty&&comparison==='pending'&&<Alert type="warning" title={t("请先保存或重新读取当前表配置，再确认结构更新。")} style={{marginBottom:16}}/>}
   <Table<SchemaChange> rowKey={change=>JSON.stringify([change.table,change.column,change.property,change.kind])} size="small" dataSource={changes} pagination={false} scroll={{x:950,y:'min(56vh, 600px)'}} columns={diffColumns}/>
   {error&&<Alert type="error" title={error}/>}
  </Modal>
  <Modal title={t("编辑名称与说明 · {0}{1}", {"0":(editor?.table??''),"1":(editor?.column?`.${editor.column}`:'')})} open={!!editor} confirmLoading={busy} onCancel={()=>setEditor(undefined)} onOk={async()=>{if(!editor)return;const value=editor;if(await run(()=>databases.annotate(id,value.table,value.column,db.revision,value.annotation),t("名称与说明已保存")))setEditor(undefined)}} okText={t("保存说明")}>
   {editor&&<Space orientation="vertical" style={{width:'100%'}}>
    <Text>{t("显示名称")}</Text><Input aria-label={t("映射名称")} maxLength={170} value={editor.annotation.display_name} onChange={e=>setEditor({...editor,annotation:{...editor.annotation,display_name:e.target.value}})}/>
    <Text>{t("数据库注释：")}{editor.source||'（空）'}</Text>
    <Checkbox checked={editor.annotation.comment===null} onChange={e=>setEditor({...editor,annotation:{...editor.annotation,comment:e.target.checked?null:editor.source}})}>{t("跟随数据库注释（取消人工覆盖）")}</Checkbox>
    <Input.TextArea aria-label={t("人工说明")} rows={5} disabled={editor.annotation.comment===null} value={editor.annotation.comment??editor.source} onChange={e=>setEditor({...editor,annotation:{...editor.annotation,comment:e.target.value}})}/>
   </Space>}
  </Modal>
 </Space>;
}

import { useEffect, useState } from 'react';
import { Alert, Button, Card, Checkbox, Col, Input, InputNumber, Row, Select, Space, Statistic, Switch, Table, Tag, Typography, message } from 'antd';
import { api, databases, type Database, type PolicyView, type TableSchema } from '../api';
const { Text, Paragraph } = Typography;

export function DatabaseManager({ id, onSaved }: { id: string; onSaved: () => void }) {
 const [db, setDB] = useState<Database>();
 const [active, setActive] = useState<string>();
 const [search,setSearch] = useState('');
 const [schema, setSchema] = useState<TableSchema>();
 const [error, setError] = useState<string>();
 const [saving, setSaving] = useState(false);
 const [dirty, setDirty] = useState(false);
 const [fragmentText, setFragmentText] = useState('');
 useEffect(() => { let live=true; databases.get(id).then(d => {if(live){setDB(d);setActive(d.config.tables[0]?.table);setFragmentText(JSON.stringify(d.config.fragments ?? {},null,2));}}).catch(e=>setError(e.message));return()=>{live=false}; }, [id]);
 useEffect(() => { setSchema(undefined);if(!active)return;let live=true;api.describe(active).then(s=>{if(live)setSchema(s)}).catch(e=>setError(e.message));return()=>{live=false}; }, [active]);
 if (!db) return <Card loading={!error}>{error && <Alert type="error" title={error}/>}</Card>;
 const policy = db.config.tables.find(p => p.table===active);
 const change = (patch: Partial<Database>) => { setDB({...db,...patch});setDirty(true); };
 const edit = (patch: Partial<PolicyView>) => change({config:{...db.config,tables:db.config.tables.map(p=>p.table===active?{...p,...patch}:p)}});
 const save = async () => {
  setSaving(true);setError(undefined);
  try {
   const fragments=JSON.parse(fragmentText);
   if(!fragments || Array.isArray(fragments) || typeof fragments!=='object' || Object.values(fragments).some(v=>typeof v!=='string')) throw new Error('片段必须是名称到 SQL 字符串的 JSON 对象');
   const saved=await databases.save({...db,config:{...db.config,fragments}});
   setDB(saved);setDirty(false);message.success('数据库访问策略已保存');onSaved();
  } catch(e) { setError((e as Error).message); } finally {setSaving(false);}
 };
 const enabled=db.config.tables.filter(p=>p.enabled!==false).length;
 return <Space orientation="vertical" size="middle" style={{width:'100%'}}>
  <Card title="数据库访问策略" extra={<Space>{dirty && <Tag color="orange">有未保存修改</Tag>}<Button type="primary" loading={saving} onClick={save} disabled={!dirty}>保存并生效</Button></Space>}>
   <Row gutter={[24,16]}><Col xs={24} md={12}><Space orientation="vertical" style={{width:'100%'}}><Text type="secondary">数据库名称</Text><Input value={db.name} onChange={e=>change({name:e.target.value})}/><Text className="mono">SQLite · {db.path}</Text></Space></Col><Col><Statistic title="已开放数据表" value={enabled} suffix={`/ ${db.config.tables.length}`}/></Col></Row>
  </Card>
  {error && <Alert type="error" showIcon title="保存或加载失败" description={error} closable onClose={()=>setError(undefined)}/>}
  <Row gutter={[16,16]}>
   <Col xs={24} xl={11}><Card title="表访问策略" extra={<Input aria-label="搜索数据表" placeholder="搜索数据表" allowClear value={search} onChange={e=>setSearch(e.target.value)} style={{width:160}}/>}>
    <Table<PolicyView> rowKey="table" size="small" dataSource={db.config.tables.filter(p=>p.table.toLowerCase().includes(search.toLowerCase()))} pagination={{pageSize:15}} scroll={{y:360}} onRow={p=>({onClick:()=>setActive(p.table),style:{cursor:'pointer',background:active===p.table?'rgba(0,113,227,.08)':undefined}})} columns={[
     {title:'数据表',dataIndex:'table',render:v=><Text className="mono">{v}</Text>},
     {title:'访问',render:(_,p)=><Tag color={p.enabled!==false?'green':'default'}>{p.enabled!==false?'开放':'禁止'}</Tag>},
     {title:'字段',render:(_,p)=>p.columns===null||p.columns===undefined?'全部':`${p.columns.length} 个`},
     {title:'行上限',render:(_,p)=>p.max_rows||'全局上限'},
    ]}/>
   </Card></Col>
   <Col xs={24} xl={13}><Card title={active?`策略 · ${active}`:'选择左侧数据表'}>
    {policy ? <Space orientation="vertical" size="middle" style={{width:'100%'}}>
     <Space><Text>允许访问</Text><Switch checked={policy.enabled!==false} onChange={v=>edit({enabled:v})}/></Space>
     <Space><Text>单次查询最多返回</Text><InputNumber min={1} max={1000} value={policy.max_rows||1000} onChange={v=>edit({max_rows:v??200})}/><Text>行</Text></Space>
     <Text type="secondary">关联多张表时采用其中最小上限；限制返回结果行数。</Text>
     <Space orientation="vertical" style={{width:'100%'}}><Text strong>可访问字段</Text>
      <Checkbox checked={policy.columns==null} onChange={e=>edit({columns:e.target.checked?null:[]})}>全部字段（包括未来新增字段）</Checkbox>
      {schema ? <Checkbox.Group style={{display:'flex',flexWrap:'wrap',gap:12}} value={policy.columns??schema.columns.map(c=>c.name)} onChange={v=>edit({columns:v as string[]})} options={schema.columns.map(c=>({value:c.name,label:`${c.name} · ${c.type}`}))}/> : <Text type="secondary">加载字段中…</Text>}
     </Space>
     <Space><Text>行权限</Text><Select value={policy.passthrough?'pass':'rewrite'} style={{width:200}} options={[{value:'pass',label:'无需行权限改写'},{value:'rewrite',label:'使用 SQL 模板改写'}]} onChange={v=>edit({passthrough:v==='pass',sql:v==='pass'?'':policy.sql||`SELECT * FROM main."${policy.table.replace(/"/g,'""')}"`})}/></Space>
     <Space><Text>方案标签</Text><Select value={policy.scheme} style={{width:200}} options={db.config.schemes.map(s=>({value:s.scheme,label:s.name}))} onChange={v=>edit({scheme:v})}/></Space>
     {!policy.passthrough && <><Paragraph type="secondary" style={{margin:0}}>使用 main.表名引用原表；支持 {'${userId}'}、{'${asOf}'} 和 {'{{片段名称}}'}。字段白名单会在行权限过滤后应用。</Paragraph><Input.TextArea className="mono" rows={8} value={policy.sql} onChange={e=>edit({sql:e.target.value})}/></>}
     <Text strong>前置权限检查（deny_unless，可选）</Text><Input.TextArea className="mono" rows={3} value={policy.deny_unless} onChange={e=>edit({deny_unless:e.target.value})}/>
     <Input placeholder="拒绝访问提示" value={policy.deny_message} onChange={e=>edit({deny_message:e.target.value})}/>
     <Input.TextArea placeholder="规则说明" value={policy.reason} onChange={e=>edit({reason:e.target.value})}/>
    </Space> : <Paragraph type="secondary">设置表是否开放、可访问字段、行权限改写方式及最大返回行数。新增数据库默认禁止所有表。</Paragraph>}
   </Card></Col>
  </Row>
  <Card title="共享 SQL 片段（当前数据库）"><Input.TextArea className="mono" rows={6} value={fragmentText} onChange={e=>{setFragmentText(e.target.value);setDirty(true)}}/></Card>
 </Space>;
}

import { useEffect, useState } from 'react';
import { Alert, Button, Card, Checkbox, Col, Collapse, Empty, Form, Input, Modal, Row, Select, Space, Table, Tabs, Tag, Typography } from 'antd';
import { DeleteOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons';
import { managementCall, type Database, type DDLChange, type DDLColumn, type DDLExecution, type DDLObject, type DDLPlan } from '../api';
import { CodeEditor } from '../components/CodeEditor';
import { SqlBlock } from '../components/shared';
import { SqlDiff } from '../components/SqlDiff';
import { useAuth } from '../Auth';
import { navigate, selectResource, setRouteParams, useNavigationBlocker, useRoute, useRouteChoice } from '../navigation';
import { useI18n } from '../ui/preferences';

const objectLabels: Record<string,string> = {table:'表',index:'索引',view:'数据库视图',trigger:'触发器',column:'字段',constraint:'约束'};
type Edit = {operation:'create'|'update'|'rename';object_type:string;name?:string;parent_name?:string;sql?:string};

export default function DDLManagement({db,onSaved}:{db:Database;onSaved:()=>void}) {
 const t=useI18n();const {user}=useAuth();const route=useRoute();
 const writable=user.system_admin||!!db.permissions?.includes('ddl.write');
 const [tab,setTab]=useRouteChoice('tab','objects',['objects','sql','history']);
 const [kind,setKind]=useRouteChoice('type','all',['all','table','index','view','trigger']);
 const [search,setSearch]=useState('');const [page,setPage]=useState(1);
 const [objects,setObjects]=useState<DDLObject[]>([]);const [total,setTotal]=useState(0);const [current,setCurrent]=useState<DDLObject>();
 const [sql,setSQL]=useState('');const [loading,setLoading]=useState(false);const [busy,setBusy]=useState(false);const [error,setError]=useState<string>();
 const [edit,setEdit]=useState<Edit>();const [form]=Form.useForm();
 const editType=Form.useWatch('object_type',form);const parent=Form.useWatch('parent_name',form);
 const [parentColumns,setParentColumns]=useState<string[]>([]);
 const [plan,setPlan]=useState<DDLPlan>();const [requestID,setRequestID]=useState('');
 const [execution,setExecution]=useState<DDLExecution>();const [history,setHistory]=useState<DDLExecution[]>([]);const [historyTotal,setHistoryTotal]=useState(0);const [historyPage,setHistoryPage]=useState(1);
 const call=<T,>(action:string,args:Record<string,unknown>={})=>managementCall<T>(action,{database_id:db.id,...args});
 useNavigationBlocker(!!sql.trim()||!!edit||!!plan,t('放弃未保存的 DDL 修改？'),busy);

 const loadObjects=async()=>{setLoading(true);try{const result=await call<{items:DDLObject[];total:number}>('list_ddl_objects',{object_type:kind==='all'?'':kind,query:search,limit:20,offset:(page-1)*20});setObjects(result.items);setTotal(result.total);}catch(e){setError((e as Error).message)}finally{setLoading(false)}};
 const loadHistory=async()=>{try{const result=await call<{items:DDLExecution[];total:number}>('list_ddl_executions',{limit:20,offset:(historyPage-1)*20});setHistory(result.items);setHistoryTotal(result.total);}catch(e){setError((e as Error).message)}};
 const loadCurrent=async()=>{if(!route.selection){setCurrent(undefined);return}try{setCurrent(await call<DDLObject>('get_ddl_object',{object_type:route.params.get('object_type')||'table',name:route.selection}))}catch(e){setCurrent(undefined);setError((e as Error).message)}};
 useEffect(()=>{const timer=setTimeout(()=>void loadObjects(),180);return()=>clearTimeout(timer)},[db.id,kind,search,page]);
 useEffect(()=>{if(tab==='history')void loadHistory()},[tab,historyPage,db.id]);
 useEffect(()=>{setCurrent(undefined);void loadCurrent()},[route.selection,route.params.get('object_type'),db.id]);
 useEffect(()=>{if(!parent){setParentColumns([]);return}let live=true;void call<DDLObject>('get_ddl_object',{object_type:'table',name:parent}).then(o=>{if(live)setParentColumns(o.definition.columns?.map(c=>c.name||'')||[])}).catch(()=>{if(live)setParentColumns([])});return()=>{live=false}},[parent]);
 const choose=(o:DDLObject)=>{setError(undefined);setRouteParams({object_type:o.object_type},true,true);selectResource(o.name)};
 const openEdit=(value:Edit)=>{setError(undefined);setEdit(value);form.resetFields();form.setFieldsValue({...value,columns:[{name:'id',type:'INTEGER',primary_key:true},{name:'name',type:'TEXT'}],index_columns:[],unique:false})};
 const preview=async(args:Record<string,unknown>)=>{setBusy(true);setError(undefined);try{const p=await call<DDLPlan>('preview_ddl',args);setPlan(p);setRequestID(crypto.randomUUID());setEdit(undefined)}catch(e){setError((e as Error).message)}finally{setBusy(false)}};
 const submitEdit=async()=>{
  try{const v=await form.validateFields();const change:DDLChange={operation:edit!.operation,object_type:v.object_type,name:v.name,parent_name:v.parent_name,new_name:v.new_name};
   if(change.operation!=='rename'){
    if(v.object_type==='table'&&change.operation==='create')change.definition={columns:v.columns.map((c:Record<string,unknown>)=>({...c,primary_key:c.primary_key?1:0,default:c.default===''?undefined:c.default})),strict:!!v.strict,without_rowid:!!v.without_rowid};
    else if(v.object_type==='index'&&!v.sql?.trim())change.definition={columns:(v.index_columns||[]).map((name:string)=>({name})),unique:!!v.unique,where:v.where};
    else change.definition={sql:v.sql};
   }
   await preview({changes:[change]});
  }catch(e){if(e instanceof Error)setError(e.message)}
 };
 const drop=(object_type:string,name:string,parent_name?:string)=>void preview({changes:[{operation:'drop',object_type,name,parent_name}]});
 const apply=async()=>{
  if(!plan)return;setBusy(true);setError(undefined);
  try{const result=await call<DDLExecution>('apply_ddl',{plan_id:plan.plan_id,request_id:requestID});setExecution(result);setPlan(undefined);setSQL('');setCurrent(undefined);selectResource(undefined);await loadObjects();await loadHistory();onSaved()}
  catch(e){setError((e as Error).message);void loadHistory()}
  finally{setBusy(false)}
 };
 const download=(text:string,name:string)=>{const url=URL.createObjectURL(new Blob([text],{type:'application/sql;charset=utf-8'}));const a=document.createElement('a');a.href=url;a.download=name;a.click();URL.revokeObjectURL(url)};
 const editTitle=edit?`${t(edit.operation==='rename'?'重命名':edit.operation==='create'?'新建':'修改')} ${t(objectLabels[edit.object_type]||edit.object_type)}`:'';
 const executionStatus=(s:string)=>t(({succeeded:'成功',failed:'失败',running:'执行中',interrupted:'已中断'} as Record<string,string>)[s]||s);
 return <Space orientation="vertical" size="middle" style={{width:'100%'}}>
  <Card title={t('DDL 管理')} extra={<Button icon={<ReloadOutlined/>} disabled={busy} onClick={()=>{void loadObjects();void loadCurrent();void loadHistory()}}>{t('刷新')}</Button>}>
   <Typography.Text type="secondary">{t('管理当前 SQLite 数据库的表、字段、约束、索引、数据库视图和触发器。修改先预览，执行前自动备份。')}</Typography.Text>
  </Card>
  {error&&<Alert type="error" showIcon title={error} description={t('执行请求中断时，请先查看执行记录，再决定后续操作。')} closable onClose={()=>setError(undefined)}/>}
  <Tabs activeKey={tab} onChange={setTab} items={[
   {key:'objects',label:t('数据库对象'),children:<Row gutter={[16,16]}>
    <Col xs={24} xl={9}><Card title={t('对象列表')} extra={writable&&<Button icon={<PlusOutlined/>} onClick={()=>openEdit({operation:'create',object_type:'table'})}>{t('新建')}</Button>}>
     <Space orientation="vertical" style={{width:'100%'}}>
      <Input.Search aria-label={t('搜索对象')} placeholder={t('搜索对象')} value={search} onChange={e=>{setSearch(e.target.value);setPage(1)}} allowClear/>
      <Select aria-label={t('对象类型')} style={{width:'100%'}} value={kind} onChange={v=>{setKind(v);setPage(1)}} options={[{value:'all',label:t('全部')},...['table','index','view','trigger'].map(value=>({value,label:t(objectLabels[value])}))]}/>
      <Table<DDLObject> size="small" rowKey={o=>`${o.object_type}:${o.name}`} loading={loading} dataSource={objects} pagination={{current:page,pageSize:20,total,onChange:setPage,showSizeChanger:false}} scroll={{x:300}} onRow={o=>({onClick:()=>choose(o),style:{cursor:'pointer',background:current?.name===o.name&&current.object_type===o.object_type?'rgba(0,113,227,.08)':undefined}})} columns={[
       {title:t('名称'),dataIndex:'name',render:(name:string)=><Typography.Text style={{overflowWrap:'anywhere'}}>{name}</Typography.Text>},
       {title:t('类型'),dataIndex:'object_type',width:100,render:(value:string)=>t(objectLabels[value]||value)},
      ]}/>
     </Space>
    </Card></Col>
    <Col xs={24} xl={15}><Card title={current?`${t(objectLabels[current.object_type])} · ${current.name}`:t('对象详情')} extra={current&&<Space wrap>
     <Button onClick={()=>download(current.sql+';\n',current.name+'.sql')}>{t('导出 DDL')}</Button>
     {writable&&<><Button onClick={()=>openEdit({operation:'update',object_type:current.object_type,name:current.name,parent_name:current.parent_name,sql:current.sql})}>{t('修改')}</Button><Button onClick={()=>openEdit({operation:'rename',object_type:current.object_type,name:current.name})}>{t('重命名')}</Button><Button danger onClick={()=>drop(current.object_type,current.name)}>{t('删除')}</Button></>}
    </Space>}>
     {!current?<Empty description={t('选择对象查看结构')}/>:<Space orientation="vertical" size="middle" style={{width:'100%'}}>
      {current.object_type==='table'&&<>
       <Space wrap>{writable&&<><Button onClick={()=>openEdit({operation:'create',object_type:'column',parent_name:current.name,sql:'"new_column" TEXT'})}>{t('新增字段')}</Button><Button onClick={()=>openEdit({operation:'create',object_type:'constraint',parent_name:current.name,sql:'CONSTRAINT constraint_name CHECK (1)'})}>{t('新增约束')}</Button><Button onClick={()=>openEdit({operation:'create',object_type:'index',parent_name:current.name})}>{t('新增索引')}</Button></>}{current.definition.strict&&<Tag>STRICT</Tag>}{current.definition.without_rowid&&<Tag>WITHOUT ROWID</Tag>}</Space>
       <Table<Partial<DDLColumn>> size="small" rowKey={c=>c.name!} dataSource={current.definition.columns} pagination={false} scroll={{x:650}} columns={[
        {title:t('字段'),dataIndex:'name'},{title:t('类型'),dataIndex:'type'},
        {title:t('约束'),render:(_,c)=>[c.primary_key?`PK ${c.primary_key}`:'',c.not_null?'NOT NULL':'',c.hidden?'GENERATED':''].filter(Boolean).join(' · ')||'—'},
        {title:t('默认值'),dataIndex:'default',render:(v:string|undefined)=>v??'—'},
        ...(writable?[{title:t('操作'),render:(_:unknown,c:Partial<DDLColumn>)=><Space size={0}><Button size="small" type="link" onClick={()=>openEdit({operation:'update',object_type:'column',name:c.name,parent_name:current.name,sql:c.sql||`"${c.name}" ${c.type}`})}>{t('修改')}</Button><Button size="small" type="link" onClick={()=>openEdit({operation:'rename',object_type:'column',name:c.name,parent_name:current.name})}>{t('重命名')}</Button><Button size="small" type="link" danger onClick={()=>drop('column',c.name!,current.name)}>{t('删除')}</Button></Space>}]:[]),
       ]}/>
       {!!current.definition.constraints?.length&&<Collapse items={[{key:'constraints',label:t('表约束'),children:<Space orientation="vertical" style={{width:'100%'}}>{current.definition.constraints.map((sql,i)=><div key={i}><SqlBlock sql={sql} format={false}/></div>)}<Typography.Text type="secondary">{t('通过“修改”编辑完整表定义，可修改或移除现有约束。')}</Typography.Text></Space>}]}/>}
      </>}
      {!!current.dependencies.length&&<Alert type="info" showIcon title={t('关联对象')} description={current.dependencies.join('、')}/>}
      <SqlBlock sql={current.sql} format={false}/>
     </Space>}
    </Card></Col>
   </Row>},
   {key:'sql',label:t('DDL SQL'),children:<Card title={t('DDL SQL')}><Space orientation="vertical" style={{width:'100%'}}>
    <Typography.Text type="secondary">{t('支持批量 CREATE / ALTER / DROP。字段类型、默认值和约束调整也可在对象详情中修改。')}</Typography.Text>
    <CodeEditor language="sql" value={sql} onChange={setSQL} height="340px"/>
    <Space><Button type="primary" disabled={!writable||!sql.trim()} loading={busy} onClick={()=>void preview({sql})}>{t('预览变更')}</Button><Button disabled={!sql.trim()} onClick={()=>download(sql,'ddl.sql')}>{t('导出 DDL')}</Button></Space>
   </Space></Card>},
   {key:'history',label:t('执行记录'),children:<Card><Table<DDLExecution> rowKey="execution_id" dataSource={history} pagination={{current:historyPage,total:historyTotal,pageSize:20,onChange:setHistoryPage,showSizeChanger:false}} scroll={{x:800}} columns={[
    {title:t('时间'),dataIndex:'started_at',render:(v:string)=>new Date(v).toLocaleString()},
    {title:t('状态'),dataIndex:'status',render:(s:string)=><Tag color={s==='succeeded'?'green':s==='running'?'blue':'red'}>{executionStatus(s)}</Tag>},
    {title:t('操作用户'),dataIndex:'actor_id'},{title:t('备份编号'),dataIndex:'backup_id',ellipsis:true},
    {title:t('操作'),render:(_,e)=><Button onClick={()=>setExecution(e)}>{t('详情')}</Button>},
   ]}/></Card>},
  ]}/>
  <Modal title={editTitle} open={!!edit} width={900} styles={{body:{maxHeight:'70vh',overflowY:'auto'}}} okText={t('预览变更')} onOk={()=>void submitEdit()} onCancel={()=>{if(!busy)setEdit(undefined)}} confirmLoading={busy} destroyOnHidden>
   {error&&<Alert type="error" showIcon title={error} style={{marginBottom:16}}/>}
   <Form className="configuration-form" form={form} layout="horizontal" labelAlign="left" labelWrap>
    <Form.Item name="object_type" label={<span className="configuration-label-text">{t('对象类型')}</span>} rules={[{required:true}]}><Select disabled={edit?.operation!=='create'||edit?.object_type==='column'||edit?.object_type==='constraint'} options={(edit?.object_type==='column'||edit?.object_type==='constraint'?[edit.object_type]:['table','index','view','trigger']).map(value=>({value,label:t(objectLabels[value])}))}/></Form.Item>
    <Form.Item name="name" label={<span className="configuration-label-text">{t('名称')}</span>} rules={[{required:true}]}><Input disabled={edit?.operation!=='create'}/></Form.Item>
    {['column','constraint','index'].includes(editType)&&<Form.Item name="parent_name" label={<span className="configuration-label-text">{t('所属表')}</span>} rules={[{required:true}]}><Input placeholder={t('物理表名')}/></Form.Item>}
    {edit?.operation==='rename'?<Form.Item name="new_name" label={<span className="configuration-label-text">{t('新名称')}</span>} rules={[{required:true}]}><Input/></Form.Item>:<>
     {editType==='table'&&edit?.operation==='create'?<>
      <Form.List name="columns" rules={[{validator:async(_,value)=>{if(!value?.length)throw new Error(t('至少添加一个字段'))}}]}>{(fields,{add,remove},{errors})=><Space orientation="vertical" style={{width:'100%'}}>{fields.map(({key,name,...rest})=><Space key={key} align="start" wrap>
       <Form.Item {...rest} name={[name,'name']} rules={[{required:true}]}><Input aria-label={t('字段名')} placeholder={t('字段名')} style={{width:150}}/></Form.Item>
       <Form.Item {...rest} name={[name,'type']} rules={[{required:true}]}><Input aria-label={t('字段类型')} placeholder="TEXT" style={{width:130}}/></Form.Item>
       <Form.Item {...rest} name={[name,'not_null']} valuePropName="checked"><Checkbox>NOT NULL</Checkbox></Form.Item>
       <Form.Item {...rest} name={[name,'primary_key']} valuePropName="checked"><Checkbox>{t('主键')}</Checkbox></Form.Item>
       <Form.Item {...rest} name={[name,'default']}><Input aria-label={t('默认值')} placeholder={t('默认值 SQL（可选）')} style={{width:170}}/></Form.Item>
       <Button aria-label={t('删除字段')} icon={<DeleteOutlined/>} onClick={()=>remove(name)}/>
      </Space>)}<Form.ErrorList errors={errors}/><Button icon={<PlusOutlined/>} onClick={()=>add({type:'TEXT'})}>{t('新增字段')}</Button></Space>}</Form.List>
      <Space style={{marginTop:16}}><Form.Item name="strict" valuePropName="checked"><Checkbox>STRICT</Checkbox></Form.Item><Form.Item name="without_rowid" valuePropName="checked"><Checkbox>WITHOUT ROWID</Checkbox></Form.Item></Space>
     </>:editType==='index'&&edit?.operation==='create'?<>
      <Form.Item name="index_columns" label={<span className="configuration-label-text">{t('索引字段')}</span>} rules={[{required:true}]}><Select mode="tags" options={parentColumns.map(value=>({value,label:value}))}/></Form.Item>
      <Form.Item name="unique" valuePropName="checked"><Checkbox>{t('唯一索引')}</Checkbox></Form.Item>
      <Form.Item name="where" label={<span className="configuration-label-text">{t('索引条件（可选）')}</span>}><Input placeholder="status = 1"/></Form.Item>
     </>:<Form.Item name="sql" label={<span className="configuration-label-text">{t(editType==='column'?'字段定义 SQL':editType==='constraint'?'约束定义 SQL':'完整 CREATE 定义 SQL')}</span>} rules={[{required:true}]}><CodeEditor language="sql" height="280px"/></Form.Item>}
    </>}
   </Form>
  </Modal>
  <Modal title={t('预览 DDL 变更')} open={!!plan} width={1050} styles={{body:{maxHeight:'70vh',overflowY:'auto'}}} okText={t('执行变更')} onOk={()=>void apply()} onCancel={()=>{if(!busy)setPlan(undefined)}} confirmLoading={busy} maskClosable={!busy}>
   {plan&&<Space orientation="vertical" style={{width:'100%'}}>
    {error&&<Alert type="error" title={error}/>}
    {plan.warnings.map((warning,i)=><Alert key={i} type="warning" showIcon title={warning}/>)}
    <Alert type="info" showIcon title={t('执行前将自动备份。完成后请审核结构变化，新表和新字段不会自动开放。')}/>
    <Tabs items={[{key:'diff',label:t('结构差异'),children:<Collapse items={plan.diff.map((d,i)=>({key:String(i),label:`${t(objectLabels[d.object_type]||d.object_type)} · ${d.name}`,children:<SqlDiff original={d.before} rewritten={d.after}/>}))}/>},{key:'sql',label:t('执行 SQL'),children:<SqlBlock sql={plan.sql.join(';\n\n')+';'} format={false}/>} ]}/>
   </Space>}
  </Modal>
  <Modal title={t('DDL 执行详情')} open={!!execution} width={1000} styles={{body:{maxHeight:'70vh',overflowY:'auto'}}} onCancel={()=>setExecution(undefined)} footer={<Space>{execution?.status==='succeeded'&&<Button onClick={()=>{setExecution(undefined);navigate('tables')}}>{t('审核结构变化')}</Button>}<Button onClick={()=>setExecution(undefined)}>{t('关闭')}</Button></Space>}>
   {execution&&<Space orientation="vertical" style={{width:'100%'}}>
    <Alert type={execution.status==='succeeded'?'success':'warning'} showIcon title={executionStatus(execution.status)} description={execution.error||t('结构已提交，请到查询配置审核并应用结构变化。')}/>
    <Typography.Text copyable>{execution.execution_id}</Typography.Text>
    {execution.backup_id&&<Typography.Text>{t('备份编号')}：{execution.backup_id}</Typography.Text>}
    <Typography.Text>{t('结构同步状态')}：{execution.discovery_status}</Typography.Text>
    <SqlBlock sql={execution.sql.join(';\n\n')+';'} format={false}/>
   </Space>}
  </Modal>
 </Space>;
}

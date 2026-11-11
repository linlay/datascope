import { useLayoutEffect, useRef, useState } from 'react';
import { Alert, Button, Checkbox, Collapse, Drawer, Input, Popconfirm, Space, Tag } from 'antd';
import { RobotOutlined, SendOutlined } from '@ant-design/icons';
import { platformRequest, type ActionDefinition, type Database, type ActionPreview } from '../api';
import { navigate } from '../navigation';
import { useI18n, usePreferences } from '../ui/preferences';
import { parameterMap, parameterRows, type ParameterRow } from './ParameterEditor';
import type { Invocation } from './ToolCallTrace';

type Parameters = ActionDefinition['parameters'];
type Suggestions = {summary?:string; question?:string; script?:string; parameters?:Parameters; arguments?:Record<string,unknown>; expected?:string; explanation?:string; repair?:string};
type Scope = {db:Database; name?:string};
let transfer: {db:string; revision:number; name:string; repair:string} | undefined;

function useAssistant({db,name}:Scope, snapshot:string) {
 const {language}=usePreferences();
 const [result,setResult]=useState<Suggestions>();
 const [error,setError]=useState('');
 const [busy,setBusy]=useState(false);
 const active=useRef<AbortController>();
 useLayoutEffect(()=>{
  setResult(undefined);setError('');setBusy(false);
  return ()=>active.current?.abort();
 },[db.id,db.revision,name,snapshot]);
 async function ask(assistant:Record<string,unknown>, extra:Record<string,unknown>={}) {
  active.current?.abort();
  const controller=new AbortController();active.current=controller;
  setError('');setResult(undefined);setBusy(true);
  try {
   const args={database_id:db.id,name,revision:db.revision,assistant:{...assistant,language},...extra};
   const data=await platformRequest<Suggestions>('/api/management/call',{method:'POST',signal:controller.signal,body:JSON.stringify({action:'assist_action',arguments:args})});
   if(!controller.signal.aborted)setResult(data);
  } catch(e){if(!controller.signal.aborted)setError((e as Error).message);}
  finally{if(!controller.signal.aborted)setBusy(false);}
 }
 return {result,error,busy,ask,setError};
}

type ChatTurn = {id:number; request:string; result?:Suggestions; error?:string; status:'pending'|'done'|'error'|'cancelled'; elapsed?:number};
export function DraftAssistant({db,name,draft,rows,apply}:Scope & {
 draft:Omit<ActionDefinition,'parameters'>; rows:ParameterRow[];
 apply:(script?:string,parameters?:Parameters)=>void;
}) {
 const t=useI18n();
 const {language}=usePreferences();
 const repair=transfer?.db===db.id && transfer?.revision===db.revision && transfer?.name===name ? transfer?.repair : '';
 const [open,setOpen]=useState(!!repair);
 const [intent,setIntent]=useState(repair||'');
 const [turns,setTurns]=useState<ChatTurn[]>([]);
 const [result,setResult]=useState<Suggestions>();
 const [error,setError]=useState('');
 const [busy,setBusy]=useState(false);
 const [useScript,setUseScript]=useState(true),[useParams,setUseParams]=useState(true);
 const active=useRef<AbortController>();
 const sequence=useRef(0);
 const bottom=useRef<HTMLDivElement>(null);
 const snapshot=JSON.stringify([db.id,db.revision,name,draft,rows,language]);
 useLayoutEffect(()=>{if(repair)transfer=undefined;},[repair]);
 useLayoutEffect(()=>{
  active.current?.abort();setBusy(false);setResult(undefined);setError('');
  setTurns(value=>value.map(turn=>turn.status==='pending'?{...turn,status:'cancelled'}:turn));
  return ()=>active.current?.abort();
 },[snapshot]);
 useLayoutEffect(()=>{if(open)bottom.current?.scrollIntoView({block:'nearest'});},[open,turns,busy]);
 function cancel(){
  active.current?.abort();setBusy(false);
  setTurns(value=>value.map(turn=>turn.status==='pending'?{...turn,status:'cancelled'}:turn));
 }
 async function generate(){
  if(busy||!intent.trim())return;
  let parameters:Parameters;
  try{parameters=parameterMap(rows);}catch(e){setError((e as Error).message);return;}
  const controller=new AbortController();active.current=controller;
  const id=++sequence.current,request=intent.trim(),start=performance.now();
  const history=turns.filter(turn=>turn.status==='done').slice(-6).flatMap(turn=>[
   {role:'user',content:turn.request}, {role:'assistant',content:JSON.stringify(turn.result)},
  ]);
  setTurns(value=>[...value,{id,request,status:'pending'}]);setIntent('');setError('');setBusy(true);setResult(undefined);
  try{
   const data=await platformRequest<Suggestions>('/api/management/call',{method:'POST',signal:controller.signal,body:JSON.stringify({action:'assist_action',arguments:{database_id:db.id,name,revision:db.revision,assistant:{mode:'draft',language,intent:request,history,draft:{...draft,parameters}}}})});
   if(controller.signal.aborted)return;
   setResult(data);setUseScript(true);setUseParams(true);
   setTurns(value=>value.map(turn=>turn.id===id?{...turn,result:data,status:'done',elapsed:Math.round(performance.now()-start)}:turn));
  }catch(e){
   if(controller.signal.aborted)return;
   setTurns(value=>value.map(turn=>turn.id===id?{...turn,error:(e as Error).message,status:'error',elapsed:Math.round(performance.now()-start)}:turn));
   setIntent(request);
  }finally{if(!controller.signal.aborted)setBusy(false);}
 }
 return <>
  <Button icon={<RobotOutlined/>} aria-label={t('AI 辅助')} aria-expanded={open} onClick={()=>setOpen(true)}>{t('AI 辅助')}</Button>
  <Drawer title={t('AI 辅助')} open={open} onClose={()=>setOpen(false)} size={720} rootClassName="action-assistant-drawer"
   footer={<Space orientation="vertical" style={{width:'100%'}}>
    {error&&<Alert type="error" title={error}/>}
    <Input.TextArea aria-label={t('动作需求')} placeholder={t('说明目标表、记录归属和允许修改的字段')} value={intent} onChange={e=>setIntent(e.target.value)} maxLength={8000} autoSize={{minRows:3,maxRows:7}} onKeyDown={e=>{if(e.key==='Enter'&&(e.ctrlKey||e.metaKey)){e.preventDefault();void generate();}}}/>
    <Space wrap><Button type="primary" icon={<SendOutlined/>} aria-label={t('生成建议')} aria-busy={busy} loading={busy} disabled={!intent.trim()} onClick={()=>void generate()}>{t('生成建议')}</Button>
     {busy&&<Button onClick={cancel}>{t('停止生成')}</Button>}
     <Button disabled={busy||!turns.length} onClick={()=>{setTurns([]);setResult(undefined);setError('');}}>{t('清空对话')}</Button>
    </Space>
   </Space>}>
   <Alert type="info" showIcon title={t('先对话，再确认应用')} description={t('建议仅修改草稿，不会自动保存或执行。思考摘要是方案说明，不是模型内部推理。')}/>
   <div className="assistant-chat" role="log" aria-label={t('AI 对话记录')} aria-live="polite">
    {!turns.length&&<p className="assistant-empty">{t('描述你希望实现的动作，也可以继续补充条件或回答 AI 的问题。')}</p>}
    {turns.map(turn=><section className="assistant-turn" key={turn.id}>
     <div className="assistant-user"><strong>{t('你')}</strong><div>{turn.request}</div></div>
     <div className="assistant-answer"><strong>AI</strong>
      <Collapse size="small" items={[{key:'trace',label:t('请求与工具调用'),children:<>
       <p><code>assist_action</code> <Tag>{t(turn.status==='pending'?'正在生成':turn.status==='done'?'已完成':turn.status==='cancelled'?'已停止':'失败')}</Tag>{turn.elapsed!==undefined&&`${turn.elapsed} ms`}</p>
       <p>{t('后台请求：读取授权结构、生成建议并校验。当前助手不调用模型工具，也不执行动作 SQL。')}</p>
      </>}]}/>
      {turn.status==='pending'&&<p role="status">{t('正在等待模型回复与校验结果…')}</p>}
      {turn.status==='cancelled'&&<p>{t('请求已停止或草稿已变化，未采用结果。')}</p>}
      {turn.error&&<Alert type="error" title={turn.error}/>}
      {turn.result&&<>
       <Collapse size="small" items={[{key:'summary',label:t('思考摘要'),children:<p className="assistant-prose">{turn.result.summary||t('模型未提供思考摘要。')}</p>}]}/>
       <div className="assistant-prose"><strong>{t('正文')}</strong><p>{turn.result.question||turn.result.explanation||t('建议已通过参数与脚本校验，请检查下方差异。')}</p></div>
       {turn.result.question&&<p>{t('在下方回复，AI 会结合本轮对话继续生成。')}</p>}
      </>}
     </div>
    </section>)}
   </div>
   {result?.script&&result.parameters&&<div className="assistant-proposal">
    <h3>{t('确认草稿变更')}</h3>
    <Checkbox checked={useParams} onChange={e=>setUseParams(e.target.checked)}>{t('采纳参数定义')}</Checkbox>
    <Comparison before={JSON.stringify(rows,null,2)} after={JSON.stringify(parameterRows(result.parameters),null,2)}/>
    <Checkbox checked={useScript} onChange={e=>setUseScript(e.target.checked)}>{t('采纳脚本')}</Checkbox>
    <Comparison before={draft.script} after={result.script}/>
    <Popconfirm title={t('确认应用到草稿？')} description={t('仅替换选中的参数和脚本，保存仍需手动操作。')} okText={t('确认应用')} cancelText={t('取消')} onConfirm={()=>{apply(useScript?result.script:undefined,useParams?result.parameters:undefined);setResult(undefined);setOpen(false);}}>
     <Button type="primary" disabled={!useScript&&!useParams}>{t('应用到草稿')}</Button>
    </Popconfirm>
   </div>}
   <div ref={bottom}/>
  </Drawer>
 </>;
}
function Comparison({before,after}:{before:string;after:string}){
 const t=useI18n();
 if(before===after)return <span>{t('内容相同')}</span>;
 const original=before.split('\n'), proposed=after.split('\n');
 let start=0,end=0;
 while(start<Math.min(original.length,proposed.length)&&original[start]===proposed[start])start++;
 while(end<Math.min(original.length,proposed.length)-start&&original[original.length-1-end]===proposed[proposed.length-1-end])end++;
 return <div className="assistant-comparison">{[original,proposed].map((lines,side)=><div key={side}>
  <strong>{t(side?'AI 建议':'当前草稿')}</strong><pre>{lines.map((line,index)=>{
   const changed=index>=start&&index<lines.length-end;
   return <span key={index} className={changed?(side?'added':'removed'):undefined}>{changed?(side?'+ ':'− '):'  '}{line}{'\n'}</span>;
  })}</pre>
 </div>)}</div>;
}

export function TrialAssistant({db,name,trial,args,identity,requestID,running,applyArgs,expected,setExpected}:Scope & {
 trial?:Invocation<ActionPreview>; args:string; identity:number; requestID:string; running:boolean;
 applyArgs:(value:string)=>void; expected:string;setExpected:(s:string)=>void;
}) {
 const t=useI18n();
 const {ask,result,error,busy}=useAssistant({db,name},JSON.stringify([args,identity,requestID,running,trial?.elapsedMS]));
 function diagnose(){
  if(trial)void ask({mode:'diagnose'},{request_id:trial.arguments.request_id,arguments:trial.error?trial.arguments.arguments:undefined});
 }
 return <Space orientation="vertical" style={{width:'100%'}}>
  <Space wrap><Button aria-label={t('AI 生成测试参数')} aria-busy={busy} disabled={running} loading={busy} onClick={()=>void ask({mode:'test'})}>{t('AI 生成测试参数')}</Button>
   {trial&&<Button disabled={running||busy} onClick={diagnose}>{t('AI 解释试运行')}</Button>}</Space>
  {error&&<Alert type="error" title={error}/>}
  {result?.question&&<Alert type="info" title={result.question}/>}
  {result?.arguments&&<><pre>{JSON.stringify(result.arguments,null,2)}</pre><p>{result.expected}</p><Button onClick={()=>{applyArgs(JSON.stringify(result.arguments,null,2));setExpected(result.expected||'');}}>{t('采用测试用例')}</Button></>}
  <label>{t('预期结果')}<Input.TextArea aria-label={t('预期结果')} value={expected} onChange={e=>setExpected(e.target.value)}/></label>
  {result?.explanation&&<Alert type="info" title={result.explanation}/>}
  {result?.repair&&name&&<><p>{result.repair}</p><Button onClick={()=>{
   transfer={db:db.id,revision:db.revision,name,repair:result.repair!};navigate('actions',name);
  }}>{t('带回动作草稿')}</Button></>}
 </Space>;
}

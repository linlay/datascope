import { useLayoutEffect, useRef, useState } from 'react';
import { Alert, Button, Checkbox, Input, Space } from 'antd';
import { platformRequest, type ActionDefinition, type Database, type ActionPreview } from '../api';
import { navigate } from '../navigation';
import { useI18n, usePreferences } from '../ui/preferences';
import { parameterMap, parameterRows, type ParameterRow } from './ParameterEditor';
import type { Invocation } from './ToolCallTrace';

type Parameters = ActionDefinition['parameters'];
type Suggestions = {question?:string; script?:string; parameters?:Parameters; arguments?:Record<string,unknown>; expected?:string; explanation?:string; repair?:string};
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

export function DraftAssistant({db,name,draft,rows,apply}:Scope & {
 draft:Omit<ActionDefinition,'parameters'>; rows:ParameterRow[];
 apply:(script?:string,parameters?:Parameters)=>void;
}) {
 const t=useI18n();
 const repair=transfer?.db===db.id && transfer?.revision===db.revision && transfer?.name===name ? transfer?.repair : '';
 const [open,setOpen]=useState(!!repair);
 const [intent,setIntent]=useState(repair||'');
 useLayoutEffect(()=>{if(repair)transfer=undefined;},[repair]);
 const [useScript,setUseScript]=useState(true),[useParams,setUseParams]=useState(true);
 const {ask,busy,result,error,setError}=useAssistant({db,name},JSON.stringify([draft,rows,intent]));
 function generate(){
  try{void ask({mode:'draft',intent,draft:{...draft,parameters:parameterMap(rows)}});}
  catch(e){setError((e as Error).message);}
 }
 return <Space orientation="vertical" style={{width:'100%'}}>
  <Button aria-expanded={open} onClick={()=>setOpen(!open)}>{t('AI 辅助')}</Button>
  {open&&<>
   <Input.TextArea aria-label={t('动作需求')} placeholder={t('说明目标表、记录归属和允许修改的字段')} value={intent} onChange={e=>setIntent(e.target.value)} maxLength={8000}/>
   <Button aria-label={t('生成建议')} aria-busy={busy} loading={busy} disabled={!intent.trim()} onClick={generate}>{t('生成建议')}</Button>
   {error&&<Alert type="error" title={error}/>}
   {result?.question&&<Alert type="info" title={result.question} description={t('请在需求中补充答案，再次生成。')}/>}
   {result?.script&&result.parameters&&<>
    <Checkbox checked={useParams} onChange={e=>setUseParams(e.target.checked)}>{t('采纳参数定义')}</Checkbox>
    <Comparison before={JSON.stringify(rows,null,2)} after={JSON.stringify(parameterRows(result.parameters),null,2)}/>
    <Checkbox checked={useScript} onChange={e=>setUseScript(e.target.checked)}>{t('采纳脚本')}</Checkbox>
    <Comparison before={draft.script} after={result.script}/>
    <Button disabled={!useScript&&!useParams} onClick={()=>apply(useScript?result.script:undefined,useParams?result.parameters:undefined)}>{t('应用到草稿')}</Button>
   </>}
  </>}
 </Space>;
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

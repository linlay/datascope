import { Input, InputNumber, Select, Space, Typography } from 'antd';
import type { Database, PolicyView } from '../api';
const {Text,Paragraph}=Typography;

// Row policies share the same draft and save operation as table/column access.
export function TablePolicyEditor({db,policy,disabled,onChange}:{db:Database;policy:PolicyView;disabled:boolean;onChange:(patch:Partial<PolicyView>)=>void}){
 return <Space orientation="vertical" size="middle" style={{width:'100%'}}>
  <Space wrap><Text>单次查询最多返回</Text><InputNumber disabled={disabled} min={1} max={1000} value={policy.max_rows||1000} onChange={value=>onChange({max_rows:value??200})}/><Text>行</Text></Space>
  <Space wrap><Text>行权限</Text><Select disabled={disabled} value={policy.passthrough?'pass':'rewrite'} style={{width:210}} options={[{value:'pass',label:'无需行权限过滤'},{value:'rewrite',label:'使用 SQL 模板过滤'}]} onChange={value=>onChange({passthrough:value==='pass',sql:value==='pass'?'':policy.sql||`SELECT * FROM main."${policy.table.replace(/"/g,'""')}"`})}/></Space>
  <Space wrap><Text>方案标签</Text><Select disabled={disabled} value={policy.scheme} style={{width:210}} options={db.config.schemes.map(s=>({value:s.scheme,label:s.name}))} onChange={scheme=>onChange({scheme})}/></Space>
  {!policy.passthrough&&<><Paragraph type="secondary" style={{margin:0}}>使用 main.表名引用原表，支持 {'${userId}'}、{'${asOf}'} 和 {'{{片段名称}}'}。字段限制在行过滤后应用。</Paragraph><Input.TextArea disabled={disabled} aria-label="行权限 SQL" className="mono" rows={7} value={policy.sql} onChange={e=>onChange({sql:e.target.value})}/></>}
  <div style={{width:'100%'}}><Text>前置权限检查（可选）</Text><Input.TextArea disabled={disabled} aria-label="前置权限检查" className="mono" rows={3} value={policy.deny_unless} onChange={e=>onChange({deny_unless:e.target.value})}/></div>
  <Input disabled={disabled} aria-label="拒绝访问提示" placeholder="拒绝访问提示" value={policy.deny_message} onChange={e=>onChange({deny_message:e.target.value})}/>
  <Input.TextArea disabled={disabled} aria-label="权限规则说明" placeholder="权限规则说明" value={policy.reason} onChange={e=>onChange({reason:e.target.value})}/>
 </Space>;
}

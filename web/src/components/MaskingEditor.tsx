import { useState } from 'react';
import { Alert, Descriptions, Input, InputNumber, Modal, Select, Space, Typography } from 'antd';
import type { Column, Masking } from '../api';
import { useI18n } from '../ui/preferences';

const { Text } = Typography;
export const maskingLabels: Record<Masking['type'], string> = {
 none:'不脱敏', full:'全部遮盖', partial:'部分遮盖', omit:'不返回', hmac:'哈希脱敏',
};

export function defaultMasking(type:Masking['type'], field:string):Masking {
 return type==='partial'?{type,keep_prefix:/name/i.test(field)?1:3,keep_suffix:/name/i.test(field)?0:4}:{type};
}

export function MaskingEditor({table,column,onCancel,onApply}: {
 table:string;column:Column;onCancel:()=>void;onApply:(rule?:Masking)=>void;
}) {
 const t=useI18n();
 const [rule,setRule]=useState<Masking>(column.masking??{type:'none'});
 const [sample,setSample]=useState(/name/i.test(column.name)?'张三丰':'13812345678');
 const prefix=rule.keep_prefix??0, suffix=rule.keep_suffix??0;
 const chars=Array.from(sample);
 let preview=sample;
 if(rule.type==='full')preview='******';
 if(rule.type==='partial')preview=chars.length<=prefix+suffix?'*'.repeat(chars.length):chars.slice(0,prefix).join('')+'*'.repeat(chars.length-prefix-suffix)+(suffix?chars.slice(-suffix).join(''):'');
 if(rule.type==='omit')preview=t('此字段不会出现在查询结果中');
 if(rule.type==='hmac')preview=t('由服务器生成稳定摘要，同字段同值保持一致');
 return <Modal open title={t('配置字段脱敏')} width={600} okText={t('应用到草稿')} cancelText={t('取消')} onCancel={onCancel} onOk={()=>onApply(rule.type==='none'?undefined:rule)}>
  <Space orientation="vertical" size="middle" style={{width:'100%'}}>
   <Descriptions size="small" column={1} items={[
    {key:'column',label:t('字段 / 类型'),children:<><Text code>{table}.{column.name}</Text><Text type="secondary"> · {column.type}</Text></>},
   ]}/>
   <Space orientation="vertical" size={4} style={{width:'100%'}}>
    <Text>{t('脱敏规则')}</Text>
    <Select aria-label={t('脱敏规则')} style={{width:'100%'}} value={rule.type} options={Object.entries(maskingLabels).map(([value,label])=>({value,label:t(label)}))} onChange={type=>setRule(defaultMasking(type,column.name))}/>
   </Space>
   {rule.type==='partial'&&<Space wrap size="large">
    <Space><Text>{t('保留前几位')}</Text><InputNumber aria-label={t('保留前几位')} min={0} max={100} precision={0} value={prefix} onChange={value=>setRule({...rule,keep_prefix:value??0})}/></Space>
    <Space><Text>{t('保留后几位')}</Text><InputNumber aria-label={t('保留后几位')} min={0} max={100} precision={0} value={suffix} onChange={value=>setRule({...rule,keep_suffix:value??0})}/></Space>
   </Space>}
   {rule.type==='partial'&&<Text type="secondary">{t('手机号可保留前 3 后 4；姓名可保留前 1 后 0。短于或等于保留总位数时全部遮盖。')}</Text>}
   {rule.type==='omit'&&<Alert type="info" showIcon title={t('不返回仅隐藏输出，字段仍可能用于筛选和关联。禁止引用请关闭字段开放权限。')}/>}
   {rule.type==='hmac'&&<Alert type="info" showIcon title={t('使用服务器密钥生成 HMAC-SHA256，同字段同值可关联；不同数据库或字段分别隔离。')}/>}
   <Space orientation="vertical" size={6} style={{width:'100%',padding:16,background:'var(--surface-secondary, rgba(127,127,127,.06))',borderRadius:8}}>
    <Text strong>{t('效果预览')}</Text>
    <Text type="secondary">{t('输入示例文本，不读取真实业务数据。')}</Text>
    <Input aria-label={t('示例文本')} maxLength={200} value={sample} onChange={e=>setSample(e.target.value)}/>
    <Text code style={{whiteSpace:'pre-wrap',overflowWrap:'anywhere'}}>{preview||t('（空）')}</Text>
   </Space>
   <Text type="secondary">{t('应用后需保存表配置。作用于普通查询、原始查询和表数据浏览；Action 返回值暂不适用。')}</Text>
  </Space>
 </Modal>;
}

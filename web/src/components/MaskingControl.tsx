import { Button, InputNumber, Select, Space, Typography } from 'antd';
import type { Column, Masking } from '../api';
import { useI18n } from '../ui/preferences';
import { defaultMasking, maskingLabels } from './MaskingEditor';

export function MaskingControl({column,disabled,onChange,onPreview}: {
 column:Column;disabled:boolean;onChange:(rule?:Masking)=>void;onPreview:()=>void;
}) {
 const t=useI18n();
 const rule=column.masking;
 return <div className="field-masking-control">
  <div className="field-masking-choice">
   <Select size="small" aria-label={t('字段 {field} 的脱敏规则',{field:column.name})} disabled={disabled} value={rule?.type??'none'} options={Object.entries(maskingLabels).map(([value,label])=>({value,label:t(label)}))} onChange={(type:Masking['type'])=>onChange(type==='none'?undefined:defaultMasking(type,column.name))}/>
   <Button size="small" type="link" aria-label={t('预览字段 {field} 的脱敏效果',{field:column.name})} onClick={onPreview}>{t('预览')}</Button>
  </div>
  {rule?.type==='partial'&&<Space size={6} wrap>
   <Typography.Text type="secondary">{t('保留前')}</Typography.Text>
   <InputNumber size="small" aria-label={t('字段 {field} 保留前几位',{field:column.name})} disabled={disabled} min={0} max={100} precision={0} value={rule.keep_prefix??0} onChange={value=>onChange({...rule,keep_prefix:value??0})}/>
   <Typography.Text type="secondary">{t('后')}</Typography.Text>
   <InputNumber size="small" aria-label={t('字段 {field} 保留后几位',{field:column.name})} disabled={disabled} min={0} max={100} precision={0} value={rule.keep_suffix??0} onChange={value=>onChange({...rule,keep_suffix:value??0})}/>
   <Typography.Text type="secondary">{t('位')}</Typography.Text>
  </Space>}
  {rule?.type==='full'&&<Typography.Text type="secondary">******</Typography.Text>}
  {rule?.type==='omit'&&<Typography.Text type="secondary">{t('查询结果不返回此字段')}</Typography.Text>}
  {rule?.type==='hmac'&&<Typography.Text type="secondary">{t('相同值返回稳定摘要')}</Typography.Text>}
 </div>;
}

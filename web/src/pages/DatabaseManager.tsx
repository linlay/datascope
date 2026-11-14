import { Collapse, Input, InputNumber, Select, Tag, Typography } from 'antd';
import type { PolicyView } from '../api';
import { useI18n } from '../ui/preferences';
import { CodeEditor } from '../components/CodeEditor';
import { HelpTip } from '../components/HelpTip';

export function TablePolicyEditor({ policy, disabled, onChange }: {
 policy: PolicyView; disabled: boolean;
 onChange: (patch: Partial<PolicyView>) => void;
}) {
 const t = useI18n();
 const sqlLabel = (title: string, sql?: string) => <span className="policy-sql-label">
  <span>{t(title)}</span>
  <Tag color={sql?.trim() ? 'blue' : undefined}>{t(sql?.trim() ? '已配置 SQL' : '未配置 SQL')}</Tag>
 </span>;
 return <section className="table-policy-editor">
  <Typography.Text strong>{t('行权限与查询限制')}</Typography.Text>
  <div className="policy-controls">
   <label className="policy-control"><span>{t('行权限')}</span><Select aria-label={t('行权限方式')} disabled={disabled} value={policy.passthrough ? 'pass' : 'rewrite'} options={[{ value: 'pass', label: t('无需行权限过滤') }, { value: 'rewrite', label: t('使用 SQL 过滤') }]} onChange={value => onChange({ passthrough: value === 'pass', sql: value === 'pass' ? '' : policy.sql || `SELECT * FROM main."${policy.table.replace(/"/g, '""')}"` })} /></label>
   <label className="policy-control"><span>{t('最多返回行数')}</span><InputNumber aria-label={t('最多返回行数')} disabled={disabled} min={1} max={1000} value={policy.max_rows || 1000} onChange={value => onChange({ max_rows: value ?? 200 })} /></label>
  </div>
  <Collapse ghost className="policy-sql-disclosure" defaultActiveKey={[]} items={[{
   key: 'row-sql', label: sqlLabel(policy.sql?.trim() ? '行过滤 SQL' : '行级过滤', policy.sql),
   extra: <span onClick={event => event.stopPropagation()}><HelpTip label={t('行过滤 SQL')}>{t('用 Schema 限定名引用原表；${userId} 表示当前员工，${asOf} 表示授权时间。每张表保存完整的权限 SQL。')}</HelpTip></span>,
   children: <CodeEditor language="sql" height="auto" minHeight="160px" wrap value={policy.sql} readOnly={disabled} onChange={sql => onChange({ sql, passthrough: !sql.trim() })} />,
  }]} />
  <Collapse className="policy-sql-disclosure" defaultActiveKey={[]} items={[{
   key: 'guard-sql', label: sqlLabel('前置查询准入 SQL', policy.deny_unless),
   children: <div className="policy-sql-field">
    <Typography.Paragraph type="secondary">{t('条件不满足时拒绝整次查询；留空则不进行这项检查。')}</Typography.Paragraph>
    <CodeEditor language="sql" height="auto" minHeight="100px" wrap value={policy.deny_unless} readOnly={disabled} onChange={deny_unless => onChange({ deny_unless })} />
    <Input aria-label={t('条件不满足时的提示')} placeholder={t('条件不满足时的提示')} disabled={disabled} value={policy.deny_message} onChange={event => onChange({ deny_message: event.target.value })} />
   </div>,
  }]} />
 </section>;
}

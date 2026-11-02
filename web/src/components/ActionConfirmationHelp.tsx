import { useState } from 'react';
import { QuestionCircleOutlined } from '@ant-design/icons';
import { Drawer, Steps, Typography } from 'antd';
import { useI18n } from '../ui/preferences';

export function ActionConfirmationHelp() {
 const t = useI18n(), [open, setOpen] = useState(false);
 return <>
  <button type="button" className="help-tip" aria-label={t('人工确认说明')} onClick={event => { event.preventDefault(); setOpen(true); }}><QuestionCircleOutlined aria-hidden /></button>
  <Drawer title={t('人工确认说明')} open={open} onClose={() => setOpen(false)} size={460} styles={{ section: { background: 'var(--bg-base)' } }}>
   <Typography.Paragraph>{t('人工确认决定正式调用动作时，是否需要本人审核后才写入业务库。它与发布状态相互独立。')}</Typography.Paragraph>
   <Typography.Title level={5}>{t('开启人工确认')}</Typography.Title>
   <Steps orientation="vertical" items={[
    { title: t('执行并回滚预览'), description: t('先运行完整动作，返回结果、SQL 写入计划和影响行数，此时不提交。') },
    { title: t('本人打开确认页'), description: t('发起操作的同一个业务账号完成身份验证，审核预览后点击“确认并执行”。确认链接有效期为 10 分钟。') },
    { title: t('重新校验后提交'), description: t('再次检查权限、配置和业务规则；结果或写入计划变化时拒绝提交。模型无法通过 MCP 代替本人确认。') },
   ]} />
   <Typography.Title level={5}>{t('关闭人工确认')}</Typography.Title>
   <Typography.Paragraph>{t('正式调用通过权限和业务规则检查后立即提交。适合允许自动执行、影响可控的操作。')}</Typography.Paragraph>
   <Typography.Title level={5}>{t('与发布、试运行的区别')}</Typography.Title>
   <Typography.Paragraph>{t('发布控制业务 MCP 能否发现和调用动作。试运行始终回滚，无论人工确认开关是否开启。涉及资金、删除或重要状态变更的动作，通常应开启人工确认。')}</Typography.Paragraph>
  </Drawer>
 </>;
}

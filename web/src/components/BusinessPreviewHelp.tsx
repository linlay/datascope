import { ExclamationCircleOutlined } from '@ant-design/icons';
import { Tooltip } from 'antd';
import { useI18n } from '../ui/preferences';

export function BusinessPreviewHelp({ label }: { label: string }) {
 const t = useI18n();
 return <Tooltip trigger={['hover', 'focus']} title={t('使用本人业务认证，通过真实 MCP 调用。Token 仅保留在当前页面内存。MCP 重启清空回执；结果未知时只查询回执并核对审计，不自动重试写入。')}>
  <button type="button" className="help-tip" aria-label={t('{0}说明', { '0': label })}><ExclamationCircleOutlined aria-hidden /></button>
 </Tooltip>;
}

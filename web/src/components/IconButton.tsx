import { Button, Tooltip, type ButtonProps } from 'antd';
import { useState } from 'react';
import { SaveOutlined } from '@ant-design/icons';
import { useI18n } from '../ui/preferences';

export function IconButton({ label, help, ...props }: Omit<ButtonProps, 'children' | 'aria-label'> & { label: string; help?: string }) {
 const [open, setOpen] = useState(false);
 return <Tooltip title={help ? `${label}：${help}` : label} trigger={['hover', 'focus']} open={open} onOpenChange={setOpen}>
  <span className="icon-button-wrapper" tabIndex={props.disabled ? 0 : undefined} onFocusCapture={() => setOpen(true)} onBlurCapture={() => setOpen(false)} onKeyDown={event => { if (event.key === 'Escape') setOpen(false); }}>
   <Button type="text" size="small" {...props} aria-label={label} />
  </span>
 </Tooltip>;
}

export function SaveButton(props: Omit<ButtonProps, 'children' | 'icon' | 'aria-label'>) {
 const t = useI18n();
 return <IconButton type="primary" {...props} label={t('保存')} icon={<SaveOutlined />} />;
}

export function useSaveModalProps() {
 const t = useI18n();
 return { okText: <SaveOutlined />, okButtonProps: { 'aria-label': t('保存'), title: t('保存') } };
}

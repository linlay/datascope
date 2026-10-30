import { Button, Tooltip, type ButtonProps } from 'antd';
import { useState } from 'react';

export function IconButton({ label, help, ...props }: Omit<ButtonProps, 'children' | 'aria-label'> & { label: string; help?: string }) {
 const [open, setOpen] = useState(false);
 return <Tooltip title={help ? `${label}：${help}` : label} trigger={['hover', 'focus']} open={open} onOpenChange={setOpen}>
  <span className="icon-button-wrapper" tabIndex={props.disabled ? 0 : undefined} onFocusCapture={() => setOpen(true)} onBlurCapture={() => setOpen(false)} onKeyDown={event => { if (event.key === 'Escape') setOpen(false); }}>
   <Button type="text" size="small" {...props} aria-label={label} />
  </span>
 </Tooltip>;
}

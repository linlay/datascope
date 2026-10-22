import { useState, type ReactNode } from 'react';
import { QuestionCircleOutlined } from '@ant-design/icons';
import { Tooltip } from 'antd';

// Help remains available to keyboard and touch users, as well as on hover.
export function HelpTip({ label, children }: { label: string; children: ReactNode }) {
  const [open, setOpen] = useState(false);
  return (
    <Tooltip title={children} trigger={['hover', 'focus']} open={open} onOpenChange={setOpen}>
      <button
        type="button"
        className="help-tip"
        aria-label={`${label}说明`}
        onClick={() => setOpen(true)}
        onKeyDown={(event) => { if (event.key === 'Escape') { setOpen(false); event.stopPropagation(); } }}
      >
        <QuestionCircleOutlined aria-hidden />
      </button>
    </Tooltip>
  );
}

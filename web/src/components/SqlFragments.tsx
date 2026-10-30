import { useEffect, useState } from 'react';
import { Alert, Button, Empty, Input, Menu, Modal, Space, Typography } from 'antd';
import { CodeEditor } from './CodeEditor';
import { HelpTip } from './HelpTip';
import { useI18n } from '../ui/preferences';

interface Fragment { name: string; sql: string }
export function SqlFragments({ value, open, disabled, onClose, onApply }: {
 value: Record<string, string>; open: boolean; disabled: boolean;
 onClose: () => void; onApply: (fragments: Record<string, string>) => void;
}) {
 const t = useI18n();
 const [items, setItems] = useState<Fragment[]>([]);
 const [active, setActive] = useState(0);
 const [error, setError] = useState('');
 useEffect(() => {
  if (open) { setItems(Object.entries(value).map(([name, sql]) => ({ name, sql }))); setActive(0); setError(''); }
 }, [open]);
 const current = items[active];
 const patch = (change: Partial<Fragment>) => setItems(old => old.map((item, index) => index === active ? { ...item, ...change } : item));
 const apply = () => {
  const fragments: Record<string, string> = Object.create(null);
  for (const item of items) {
   if (!/^[A-Za-z0-9_]+$/.test(item.name) || Object.prototype.hasOwnProperty.call(fragments, item.name)) { setError(t('片段名称只能包含字母、数字和下划线，且不能重复。')); return; }
   if (!item.sql.trim()) { setError(t('请输入片段 SQL。')); return; }
   fragments[item.name] = item.sql;
  }
  onApply(fragments); onClose();
 };
 return <Modal title={<Space>{t('SQL 片段')}<HelpTip label={t('SQL 片段')}>{t('把多条权限规则重复使用的 SQL 定义一次，再用 {{片段名称}} 引用。例如多个表共用同一段“可见客户”规则。')}</HelpTip></Space>} open={open} onCancel={onClose} onOk={apply} okText={t('应用到当前配置')} okButtonProps={{ disabled }} width={1080} styles={{ body: { maxHeight: '76vh', overflowY: 'auto' } }}>
  {error && <Alert type="error" title={error} style={{ marginBottom: 12 }} />}
  <div className="fragment-editor-grid">
   <div><Button disabled={disabled} onClick={() => { setItems([...items, { name: '', sql: '' }]); setActive(items.length); setError(''); }}>{t('新增片段')}</Button><Menu style={{ marginTop: 12 }} selectedKeys={[String(active)]} items={items.map((item, index) => ({ key: String(index), label: item.name || t('未命名片段') }))} onClick={({ key }) => setActive(Number(key))} /></div>
   {current ? <Space orientation="vertical" size="middle" style={{ width: '100%', minWidth: 0 }}>
    <div className="query-toolbar"><Input aria-label={t('片段名称')} placeholder={t('片段名称')} value={current.name} disabled={disabled} onChange={event => patch({ name: event.target.value })} style={{ flex: 1, minWidth: 140 }} /><Button danger disabled={disabled} onClick={() => Modal.confirm({ title: t('删除此 SQL 片段？'), onOk: () => { setItems(items.filter((_, index) => index !== active)); setActive(Math.max(0, active - 1)); } })}>{t('删除')}</Button></div>
    {current.name && <Typography.Text copyable={{ text: `{{${current.name}}}` }} code>{`{{${current.name}}}`}</Typography.Text>}
    <CodeEditor language="sql" height="330px" wrap readOnly={disabled} value={current.sql} onChange={sql => patch({ sql })} />
   </Space> : <Empty description={t('暂无 SQL 片段')} />}
  </div>
 </Modal>;
}

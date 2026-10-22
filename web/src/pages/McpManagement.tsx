import { useEffect, useState } from 'react';
import { Alert, Button, Card, Checkbox, Space, Switch, Tag, Typography, message } from 'antd';
import { databases, type Database } from '../api';
import { McpInspector } from './McpInspector';

const { Text } = Typography;

export function McpManagement({ id, onSaved }: { id: string; onSaved: () => void }) {
  const [db, setDB] = useState<Database>();
  const [enabled, setEnabled] = useState(false);
  const [tools, setTools] = useState<string[]>([]);
  const [error, setError] = useState<string>();
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    let live = true;
    databases.get(id).then((item) => {
      if (!live) return;
      setDB(item);
      setEnabled(item.mcp_enabled);
      setTools(item.tools);
    }).catch((e: Error) => { if (live) setError(e.message); });
    return () => { live = false; };
  }, [id]);

  if (!db) return <Card loading={!error}>{error && <Alert type="error" title={error} />}</Card>;

  const dirty = enabled !== db.mcp_enabled || tools.length !== db.tools.length || tools.some((tool) => !db.tools.includes(tool));
  const save = async () => {
    setSaving(true);
    setError(undefined);
    try {
      const saved = await databases.save({ ...db, mcp_enabled: enabled, tools });
      setDB(saved);
      setEnabled(saved.mcp_enabled);
      setTools(saved.tools);
      message.success('MCP 配置已保存，测试接口已更新');
      onSaved();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setSaving(false);
    }
  };

  return (
    <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
      <Card title={`MCP 配置 · ${db.name}`} extra={
        <Space>
          {dirty && <Tag color="orange">有未保存修改</Tag>}
          <Button type="primary" loading={saving} disabled={!dirty} onClick={save}>保存并生效</Button>
        </Space>
      }>
        <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
          <Space>
            <Switch aria-label="开启 MCP" checked={enabled} onChange={setEnabled} />
            <Text strong>{enabled ? '开启 MCP' : '关闭 MCP'}</Text>
            <Tag>当前数据库独立配置</Tag>
          </Space>
          <Text copyable className="mono">{`/mcp/${db.id}`}</Text>
          <Checkbox.Group value={tools} onChange={(values) => setTools(values as string[])} options={[
            { value: 'list_tables', label: 'list_tables · 表目录' },
            { value: 'describe_table', label: 'describe_table · 字段结构' },
            { value: 'query', label: 'query · 权限查询' },
          ]} />
          <Text type="secondary">保存后，下方测试会刷新为实际启用的接口。表、字段和行权限请在“数据库访问策略”中配置。</Text>
        </Space>
      </Card>
      {error && <Alert type="error" showIcon title="MCP 配置保存失败" description={error} closable onClose={() => setError(undefined)} />}
      {db.mcp_enabled ? (
        <McpInspector key={db.revision} />
      ) : (
        <Alert type="info" showIcon title="MCP 测试未开启" description="为当前数据库开启 MCP 并保存后，即可在此查看实际接口、测试工具调用。" />
      )}
    </Space>
  );
}

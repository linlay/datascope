import { useEffect, useState } from 'react';
import { Alert, Button, Card, Checkbox, Input, Space, Switch, Tag, Typography, message } from 'antd';
import { databases, type Database } from '../api';
import { McpInspector } from './McpInspector';
import { HelpTip } from '../components/HelpTip';

const { Text } = Typography;
const toolOptions = [
  { name:'search_knowledge',label:'检索知识摘要',help:'按业务问题检索知识的名称、摘要和 ID。' },
 { name:'read_knowledge',label:'按需读取知识',help:'通过 ID 读取业务知识正文，支持渐进式披露。' },
 { name: 'list_tables', label: '表目录', help: '列出当前数据库允许访问的数据表。' },
  { name: 'describe_table', label: '表结构', help: '查看指定表允许访问的字段、类型和访问方案。' },
  { name: 'query', label: '权限查询', help: '只读查询，应用表、字段、行权限和返回行数限制。规则在“数据库访问策略”中配置。' },
  { name: 'raw_query', label: '原始查询', admin: true, help: '绕过表、字段和行权限改写，仍保持只读。需要当前数据库的全量查询权限。' },
  { name: 'customer_scope', label: '客户范围', admin: true, help: '核对证券样例库的客户可见范围，需要当前数据库的全量查询权限。' },
];

export function McpManagement({ id, onSaved }: { id: string; onSaved: () => void }) {
  const [db, setDB] = useState<Database>();
  const [enabled, setEnabled] = useState(false);
  const [tools, setTools] = useState<string[]>([]);
  const [mcpPath, setMcpPath] = useState('');
 const [businessText,setBusinessText]=useState('[]');
  const [error, setError] = useState<string>();
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    let live = true;
    databases.get(id).then((item) => {
      if (!live) return;
      setDB(item);
      setEnabled(item.mcp_enabled);
      setTools(item.tools);
      setMcpPath(item.mcp_path || item.id);
 setBusinessText(JSON.stringify(item.business_tools||[],null,2));
    }).catch((e: Error) => { if (live) setError(e.message); });
    return () => { live = false; };
  }, [id]);

  if (!db) return <Card loading={!error}>{error && <Alert type="error" title={error} />}</Card>;

  const pathValid = /^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$/.test(mcpPath);
  const endpoint = new URL(db.mcp_endpoint || `/mcp/${db.mcp_path || db.id}`, window.location.origin);
  const prefix = endpoint.pathname.slice(0, endpoint.pathname.lastIndexOf('/') + 1);
  const previewEndpoint = `${endpoint.origin}${prefix}${mcpPath}`;
  const dirty = businessText!==JSON.stringify(db.business_tools||[],null,2) || mcpPath !== (db.mcp_path || db.id) || enabled !== db.mcp_enabled || tools.length !== db.tools.length || tools.some((tool) => !db.tools.includes(tool));
  const save = async () => {
    setSaving(true);
    setError(undefined);
    try {
      const business_tools:unknown=JSON.parse(businessText);if(!Array.isArray(business_tools))throw new Error("业务工具配置必须是 JSON 数组");
 const saved = await databases.save({ ...db, mcp_enabled: enabled, mcp_path: mcpPath, tools, business_tools });
      setDB(saved);
      setEnabled(saved.mcp_enabled);
      setTools(saved.tools);
      setMcpPath(saved.mcp_path || saved.id);
 setBusinessText(JSON.stringify(saved.business_tools||[],null,2));
      message.success('MCP 配置已保存');
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
          <Button type="primary" loading={saving} disabled={!dirty || !pathValid} onClick={save}>保存并生效</Button>
        </Space>
      }>
        <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
          <Space>
            <Switch aria-label="开启 MCP" checked={enabled} onChange={setEnabled} />
            <Text strong>{enabled ? '开启 MCP' : '关闭 MCP'}</Text>
          </Space>
          <Space orientation="vertical" style={{ width: '100%' }}>
            <Space size={4}><Text strong>MCP 路径</Text><HelpTip label="MCP 路径">路径在所有数据库间唯一，允许字母、数字、下划线和连字符，最多 64 位。保存后立即生效，旧的分库地址失效，测试入口自动更新。</HelpTip></Space>
            <Input aria-label="MCP 路径" addonBefore={prefix} value={mcpPath} onChange={(e) => setMcpPath(e.target.value)} status={!pathValid ? 'error' : undefined} placeholder="自定义路径，例如 securities" maxLength={64} />
            {!pathValid && <Text type="danger">请输入字母或数字开头的路径，仅允许字母、数字、下划线和连字符，最多 64 位。</Text>}
            <Text copyable={pathValid ? { text: previewEndpoint } : false} className="mono">{previewEndpoint}</Text>
          </Space>
          <Space orientation="vertical" style={{ width: '100%' }}>
            <Space size={4}><Text strong>开放工具</Text><HelpTip label="开放工具">按当前数据库独立启停。保存后工具目录与调用权限立即更新，下方测试会显示实际开放的工具。</HelpTip></Space>
            <Checkbox.Group value={tools} onChange={(values) => setTools(values as string[])} className="mcp-tool-options">
              {toolOptions.map((tool) => (
                <div className="mcp-tool-option" key={tool.name}>
                  <Checkbox value={tool.name}><span className="mono">{tool.name}</span> · {tool.label}</Checkbox>
                  {tool.admin && <Tag color="orange">管理员</Tag>}
                  <HelpTip label={tool.name}>{tool.help}</HelpTip>
                </div>
              ))}
            </Checkbox.Group>
            {tools.includes('raw_query') && <Alert type="warning" showIcon title="原始查询绕过访问策略，需要单独授予全量查询权限。" />}
          </Space>
        </Space>
      </Card>
      <Card title="配置生成业务工具"><Typography.Paragraph>每个工具包含名称、说明、SQL 模板和参数定义。SQL 使用 <Typography.Text code>:arg_参数名</Typography.Text>；参数独立绑定，查询仍经过权限改写与审计。</Typography.Paragraph><Input.TextArea aria-label="业务工具 JSON 配置" className="mono" rows={12} value={businessText} onChange={e=>setBusinessText(e.target.value)}/><Typography.Paragraph type="secondary" style={{marginTop:8}}>示例：{`[{"name":"find_customer","description":"按客户编号查询","sql":"SELECT * FROM customers WHERE customer_no=:arg_customer_no","parameters":{"customer_no":{"type":"string","description":"客户编号"}}}]`}</Typography.Paragraph></Card>
      {error && <Alert type="error" showIcon title="MCP 配置保存失败" description={error} closable onClose={() => setError(undefined)} />}
      {db.authentication?.type === "local" ? <Alert type="info" showIcon title="此数据库使用独立业务认证" description="请在业务连接器中使用当前数据库的工号和密码完成 OAuth 授权后测试；平台管理账号不能代替员工登录。" /> : db.mcp_enabled ? (
        <McpInspector key={db.revision} />
      ) : (
        <Alert type="info" showIcon title="MCP 已关闭，开启并保存后可测试。" />
      )}
    </Space>
  );
}

import { useEffect, useMemo, useState } from 'react';
import { Alert, Button, Card, Descriptions, Empty, Input, Select, Space, Tag, Typography } from 'antd';
import { api, type CallResult, type McpTool, type ServerInfo } from '../api';
import { ResultTable, SqlBlock, UserPicker } from '../components/shared';
import { HelpTip } from '../components/HelpTip';

const { Paragraph, Text } = Typography;

export function McpInspector() {
  const [info, setInfo] = useState<ServerInfo>();
  const [infoError, setInfoError] = useState<string>();
  const [loadingInfo, setLoadingInfo] = useState(false);

  const loadInfo = () => {
    setLoadingInfo(true);
    api
      .mcpInfo()
      .then((i) => {
        setInfo(i);
        setInfoError(undefined);
      })
      .catch((e: Error) => setInfoError(e.message))
      .finally(() => setLoadingInfo(false));
  };
  useEffect(loadInfo, []);

  return (
    <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
      <Card size="small" title="连接状态" extra={<Button onClick={loadInfo} loading={loadingInfo}>刷新</Button>}>
        {infoError && <Alert type="error" showIcon message="无法连接 MCP server" description={infoError} />}
        {info && (
          <>
          <Space wrap>
            <Tag color={infoError ? 'default' : loadingInfo ? 'processing' : 'success'}>{infoError ? '上次连接' : loadingInfo ? '检查中' : '已连接'}</Tag>
            <Text>{info.auth_mode === 'oauth' ? 'OAuth' : '可信身份头'}</Text>
            <Text type="secondary">协议 {info.protocol_version}</Text>
            <Text type="secondary">{info.tools.length} 个工具 · {info.elapsed_ms} ms</Text>
          </Space>
          <details className="mcp-connection-details">
          <summary>连接详情</summary>
          <Descriptions
            size="small"
            column={{ xs: 1, md: 2, xl: 3 }}
            items={[
              { key: 'endpoint', label: '端点', children: <span className="mono">{info.endpoint}</span> },
              { key: 'server', label: '服务', children: `${info.server.title ?? info.server.name} v${info.server.version}` },
              {
                key: 'protocol',
                label: '协商协议',
                children: (
                  <Tag color={info.protocol_version === info.latest_protocol_version ? 'success' : 'warning'}>
                    {info.protocol_version}
                    {info.protocol_version === info.latest_protocol_version ? '（最新）' : `（最新为 ${info.latest_protocol_version}）`}
                  </Tag>
                ),
              },
              { key: 'header', label: '身份来源', children: <span className="mono">{info.auth_mode === 'oauth' ? 'OAuth · 员工令牌' : info.user_header}</span> },
              {
                key: 'tools',
                label: '工具',
                children: (
                  <Space wrap>
                    {info.tools.map((t) => (
                      <Tag key={t.name} color={['raw_query', 'customer_scope'].includes(t.name) ? 'orange' : 'blue'}>
                        {t.name}
                      </Tag>
                    ))}
                  </Space>
                ),
              },
              { key: 'elapsed', label: '连接耗时', children: `${info.elapsed_ms} ms` },
              { key: 'instructions', label: '服务说明', span: 3, children: <Paragraph style={{ whiteSpace: 'pre-wrap', margin: 0 }}>{info.instructions}</Paragraph> },
            ]}
          />
          </details>
          </>
        )}
      </Card>
      <ToolPlayground tools={info?.tools ?? []} />
    </Space>
  );
}

function defaultArguments(tool?: McpTool): string {
  const props = tool?.inputSchema?.properties ?? {};
  const args: Record<string, unknown> = {};
  if ('sql' in props) args.sql = 'SELECT customer_no, name, customer_type FROM customers ORDER BY customer_no';
  if ('table' in props) args.table = 'customers';
  if ('user_id' in props) args.user_id = 1;
  return JSON.stringify(args, null, 2);
}

function ToolPlayground({ tools }: { tools: McpTool[] }) {
  const [name, setName] = useState<string>();
  const [userId, setUserId] = useState(1);
  const [argsText, setArgsText] = useState('{}');
  const [result, setResult] = useState<CallResult>();
  const [error, setError] = useState<string>();
  const [calling, setCalling] = useState(false);

  const tool = useMemo(() => tools.find((t) => t.name === name), [tools, name]);
  useEffect(() => {
    if (!name && tools.length) setName(tools.find((t) => t.name === 'query')?.name ?? tools[0].name);
  }, [tools, name]);
  useEffect(() => setArgsText(defaultArguments(tool)), [tool]);

  const call = () => {
    let args: Record<string, unknown>;
    try {
      args = JSON.parse(argsText);
    } catch {
      setError('参数不是合法 JSON');
      return;
    }
    setCalling(true);
    api
      .mcpCall(name!, args, userId)
      .then((r) => {
        setResult(r);
        setError(undefined);
      })
      .catch((e: Error) => setError(e.message))
      .finally(() => setCalling(false));
  };

  const structured = result?.result.structuredContent as
    | { columns?: string[]; rows?: Record<string, unknown>[]; rewritten_sql?: string; row_count?: number }
    | undefined;

  return (
    <Card size="small" title={<Space size={4}>工具测试<HelpTip label="工具测试">以所选员工身份调用真实 MCP 接口，服务端验证身份和权限。</HelpTip></Space>}>
      {tools.length === 0 ? (
        <Empty description="未获取到工具列表" />
      ) : (
        <Space orientation="vertical" style={{ width: '100%' }}>
          <Space wrap>
            <Select aria-label="测试工具" value={name} onChange={setName} style={{ width: 200 }} options={tools.map((t) => ({ value: t.name, label: t.name }))} />
            {tool?.description && <HelpTip label="当前工具">{tool.description}</HelpTip>}
            <Text type="secondary">身份</Text>
            <UserPicker value={userId} onChange={setUserId} />
            <Button type="primary" onClick={call} loading={calling}>
              调用
            </Button>
          </Space>
          <Input.TextArea aria-label="工具参数 JSON" className="mono" value={argsText} onChange={(e) => setArgsText(e.target.value)} autoSize={{ minRows: 3, maxRows: 10 }} />
          {error && <Alert type="error" showIcon message={error} />}
          {result && (
            <Space orientation="vertical" style={{ width: '100%' }}>
              <Space>
                {result.result.isError ? <Tag color="error">isError</Tag> : <Tag color="success">成功</Tag>}
                <Text type="secondary">
                  员工 #{result.user_id} · {result.elapsed_ms} ms
                  {structured?.row_count !== undefined && ` · ${structured.row_count} 行`}
                </Text>
              </Space>
              {result.result.isError && <Alert type="warning" showIcon message={result.result.content?.map((c) => c.text).join('\n')} />}
              {structured?.rewritten_sql && (
                <details>
                  <summary>MCP 返回的改写后 SQL（调试模式）</summary>
                  <SqlBlock sql={structured.rewritten_sql} />
                </details>
              )}
              {structured?.columns && structured.rows && <ResultTable columns={structured.columns} rows={structured.rows} />}
              <details>
                <summary>原始 JSON 响应</summary>
                <pre className="sql-block">{JSON.stringify(result.result, null, 2)}</pre>
              </details>
            </Space>
          )}
        </Space>
      )}
    </Card>
  );
}

import { useI18n } from '../ui/preferences';
import { useEffect, useMemo, useState } from 'react';
import { Alert, Button, Card, Descriptions, Empty, Input, Select, Space, Tag, Typography } from 'antd';
import { api, type CallResult, type McpTool, type ServerInfo } from '../api';
import { ResultTable, SqlBlock } from '../components/shared';
import { HelpTip } from '../components/HelpTip';

const { Paragraph, Text } = Typography;

export function McpInspector() {
 const t=useI18n();
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
      <Card size="small" title={t("连接状态")} extra={<Button onClick={loadInfo} loading={loadingInfo}>{t("刷新")}</Button>}>
        {infoError && <Alert type="error" showIcon message={t("无法连接 MCP server")} description={infoError} />}
        {info && (
          <>
          <Space wrap>
            <Tag color={infoError ? 'default' : loadingInfo ? 'processing' : 'success'}>{infoError ? t("上次连接") : loadingInfo ? t("检查中") : t("已连接")}</Tag>
            <Text>{info.auth_mode === 'oauth' ? 'OAuth' : t("可信身份头")}</Text>
            <Text type="secondary">{t("协议")}{info.protocol_version}</Text>
            <Text type="secondary">{info.tools.length} {t("个工具 ·")}{info.elapsed_ms} ms</Text>
          </Space>
          <details className="mcp-connection-details">
          <summary>{t("连接详情")}</summary>
          <Descriptions
            size="small"
            column={{ xs: 1, md: 2, xl: 3 }}
            items={[
              { key: 'endpoint', label: t("端点"), children: <span className="mono">{info.endpoint}</span> },
              { key: 'server', label: t("服务"), children: `${info.server.title ?? info.server.name} v${info.server.version}` },
              {
                key: 'protocol',
                label: t("协商协议"),
                children: (
                  <Tag color={info.protocol_version === info.latest_protocol_version ? 'success' : 'warning'}>
                    {info.protocol_version}
                    {info.protocol_version === info.latest_protocol_version ? t("（最新）") : t("（最新为 {0}）", {"0":(info.latest_protocol_version)})}
                  </Tag>
                ),
              },
              { key: 'header', label: t("身份来源"), children: <span className="mono">{info.auth_mode === 'oauth' ? t("OAuth · 员工令牌") : info.user_header}</span> },
              {
                key: 'tools',
                label: t("工具"),
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
              { key: 'elapsed', label: t("连接耗时"), children: `${info.elapsed_ms} ms` },
              { key: 'instructions', label: t("服务说明"), span: 3, children: <Paragraph style={{ whiteSpace: 'pre-wrap', margin: 0 }}>{info.instructions}</Paragraph> },
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
 const t=useI18n();
  const [name, setName] = useState<string>();
  const userId=0;
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
      setError(t("参数不是合法 JSON"));
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
    <Card size="small" title={<Space size={4}>{t("工具测试")}<HelpTip label={t("工具测试")}>{t("以当前平台账号调用真实 MCP 接口，服务端验证身份和权限。")}</HelpTip></Space>}>
      {tools.length === 0 ? (
        <Empty description={t("未获取到工具列表")} />
      ) : (
        <Space orientation="vertical" style={{ width: '100%' }}>
          <Space wrap>
            <Select aria-label={t("测试工具")} value={name} onChange={setName} style={{ width: 200 }} options={tools.map((t) => ({ value: t.name, label: t.name }))} />
            {tool?.description && <HelpTip label={t("当前工具")}>{tool.description}</HelpTip>}
            <Text type="secondary">{t("身份")}</Text>
            <Tag>{t("使用当前平台账号与已绑定的业务身份")}</Tag>
            <Button type="primary" onClick={call} loading={calling}>
              {t("调用")}</Button>
          </Space>
          <Input.TextArea aria-label={t("工具参数 JSON")} className="mono" value={argsText} onChange={(e) => setArgsText(e.target.value)} autoSize={{ minRows: 3, maxRows: 10 }} />
          {error && <Alert type="error" showIcon message={error} />}
          {result && (
            <Space orientation="vertical" style={{ width: '100%' }}>
              <Space>
                {result.result.isError ? <Tag color="error">isError</Tag> : <Tag color="success">{t("成功")}</Tag>}
                <Text type="secondary">
                  {t("员工 #")}{result.user_id} · {result.elapsed_ms} ms
                  {structured?.row_count !== undefined && t(" · {0} 行", {"0":(structured.row_count)})}
                </Text>
              </Space>
              {result.result.isError && <Alert type="warning" showIcon message={result.result.content?.map((c) => c.text).join('\n')} />}
              {structured?.rewritten_sql && (
                <details>
                  <summary>{t("MCP 返回的改写后 SQL（调试模式）")}</summary>
                  <SqlBlock sql={structured.rewritten_sql} />
                </details>
              )}
              {structured?.columns && structured.rows && <ResultTable columns={structured.columns} rows={structured.rows} />}
              <details>
                <summary>{t("原始 JSON 响应")}</summary>
                <pre className="sql-block">{JSON.stringify(result.result, null, 2)}</pre>
              </details>
            </Space>
          )}
        </Space>
      )}
    </Card>
  );
}

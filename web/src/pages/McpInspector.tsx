import { useEffect, useMemo, useState } from 'react';
import { Alert, Button, Card, Col, Descriptions, Empty, Input, Row, Select, Space, Statistic, Table, Tag, Typography } from 'antd';
import { api, type CallResult, type Check, type CheckStatus, type McpTool, type Report, type ServerInfo } from '../api';
import { ResultTable, SqlBlock, UserPicker } from '../components/shared';

const { Paragraph, Text } = Typography;

const statusMeta: Record<CheckStatus, { color: string; label: string }> = {
  pass: { color: 'success', label: '通过' },
  fail: { color: 'error', label: '失败' },
  warn: { color: 'warning', label: '警告' },
  skip: { color: 'default', label: '跳过' },
};

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
      <Card size="small" title="MCP server" extra={<Button onClick={loadInfo} loading={loadingInfo}>刷新</Button>}>
        {infoError && <Alert type="error" showIcon message="无法连接 MCP server" description={infoError} />}
        {info && (
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
        )}
      </Card>
      <VerifyCard />
      <ToolPlayground tools={info?.tools ?? []} />
    </Space>
  );
}

function VerifyCard() {
  const [report, setReport] = useState<Report>();
  const [error, setError] = useState<string>();
  const [running, setRunning] = useState(false);

  const run = () => {
    setRunning(true);
    api
      .mcpVerify()
      .then((r) => {
        setReport(r);
        setError(undefined);
      })
      .catch((e: Error) => setError(e.message))
      .finally(() => setRunning(false));
  };

  return (
    <Card
      size="small"
      title="一键校验：MCP 返回是否正确"
      extra={
        <Button type="primary" onClick={run} loading={running}>
          运行校验
        </Button>
      }
    >
      <Paragraph type="secondary">
        后台以 MCP 客户端身份连接 MCP server，模拟网关注入不同用户身份，逐项比对：协议版本、工具目录、表结构、超级管理员结果与原始数据、改写验收案例、权限案例，以及只读与拒绝规则。
      </Paragraph>
      {error && <Alert type="error" showIcon message={error} />}
      {report && (
        <Space orientation="vertical" style={{ width: '100%' }}>
          <Row gutter={24}>
            <Col><Statistic title="通过" value={report.passed} styles={{ content: { color: '#389e0d' } }} /></Col>
            <Col><Statistic title="失败" value={report.failed} styles={{ content: { color: report.failed ? '#cf1322' : undefined } }} /></Col>
            <Col><Statistic title="警告" value={report.warned} styles={{ content: { color: report.warned ? '#d48806' : undefined } }} /></Col>
            <Col><Statistic title="跳过" value={report.skipped} /></Col>
            <Col><Statistic title="耗时" value={report.elapsed_ms} suffix="ms" /></Col>
          </Row>
          <Table<Check & { key: number }>
            size="small"
            pagination={false}
            dataSource={report.checks.map((c, i) => ({ ...c, key: i }))}
            expandable={{
              rowExpandable: (c) => c.expected !== undefined || c.actual !== undefined,
              expandedRowRender: (c) => (
                <Row gutter={16}>
                  <Col span={12}>
                    <Text strong>预期</Text>
                    <pre className="sql-block">{JSON.stringify(c.expected, null, 2)}</pre>
                  </Col>
                  <Col span={12}>
                    <Text strong>MCP 实际返回</Text>
                    <pre className="sql-block">{JSON.stringify(c.actual, null, 2)}</pre>
                  </Col>
                </Row>
              ),
            }}
            columns={[
              { title: '分组', dataIndex: 'group', width: 110 },
              { title: '检查项', dataIndex: 'name', width: 320 },
              {
                title: '结果',
                dataIndex: 'status',
                width: 80,
                filters: Object.entries(statusMeta).map(([value, m]) => ({ value, text: m.label })),
                onFilter: (value, c) => c.status === value,
                render: (s: CheckStatus) => <Tag color={statusMeta[s].color}>{statusMeta[s].label}</Tag>,
              },
              { title: '说明', dataIndex: 'detail', ellipsis: true },
            ]}
          />
        </Space>
      )}
    </Card>
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
    <Card size="small" title="工具调试：以指定用户身份调用 MCP 工具">
      {tools.length === 0 ? (
        <Empty description="未获取到工具列表" />
      ) : (
        <Space orientation="vertical" style={{ width: '100%' }}>
          <Space wrap>
            <Select value={name} onChange={setName} style={{ width: 200 }} options={tools.map((t) => ({ value: t.name, label: t.name }))} />
            <Text type="secondary">身份</Text>
            <UserPicker value={userId} onChange={setUserId} />
            <Button type="primary" onClick={call} loading={calling}>
              调用
            </Button>
          </Space>
          {tool?.description && <Text type="secondary">{tool.description}</Text>}
          <Input.TextArea className="mono" value={argsText} onChange={(e) => setArgsText(e.target.value)} autoSize={{ minRows: 3, maxRows: 10 }} />
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

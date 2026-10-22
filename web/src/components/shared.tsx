import { useEffect, useMemo, useState } from 'react';
import { Select, Table, Tag, Tooltip, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { api, type Row, type Scheme, type UserSummary } from '../api';
import { formatSQL } from '../sqlFormat';

const { Text } = Typography;

export const schemeMeta: Record<Scheme, { label: string; color: string }> = {
  NONE: { label: '不改写', color: 'default' },
  EMPLOYEE: { label: '员工权限', color: 'blue' },
  CUSTOMER: { label: '客户权限', color: 'green' },
  RESTRICTED: { label: '敏感元数据', color: 'red' },
};

export function SchemeTag({ scheme }: { scheme: Scheme }) {
  const meta = schemeMeta[scheme];
  return meta ? <Tag color={meta.color}>{meta.label}</Tag> : <Tag>{scheme}</Tag>;
}

const money = new Intl.NumberFormat('zh-CN', { style: 'currency', currency: 'CNY' });

export function CellValue({ column, value }: { column: string; value: unknown }) {
  if (value === null || value === undefined) return <Text type="secondary">NULL</Text>;
  if (column.endsWith('_cents') && typeof value === 'number') {
    return (
      <Tooltip title={`${value} 分`}>
        <span className="mono" style={{ color: value < 0 ? '#cf1322' : undefined }}>
          {money.format(value / 100)}
        </span>
      </Tooltip>
    );
  }
  if (typeof value === 'object') return <span className="mono">{JSON.stringify(value)}</span>;
  return <span className={typeof value === 'number' ? 'mono' : undefined}>{String(value)}</span>;
}

export function ResultTable({
  columns,
  rows,
  loading,
  pageSize = 20,
}: {
  columns: string[];
  rows: Row[];
  loading?: boolean;
  pageSize?: number;
}) {
  const tableColumns: ColumnsType<Row> = columns.map((name) => ({
    title: <span className="mono">{name}</span>,
    dataIndex: name,
    key: name,
    ellipsis: true,
    render: (value: unknown) => <CellValue column={name} value={value} />,
  }));
  return (
    <Table<Row>
      size="small"
      bordered
      loading={loading}
      columns={tableColumns}
      dataSource={rows.map((row, i) => ({ ...row, __key: i }))}
      rowKey="__key"
      scroll={{ x: 'max-content' }}
      pagination={{ pageSize, showSizeChanger: true, hideOnSinglePage: true, showTotal: (n) => `${n} 行` }}
    />
  );
}

// SqlBlock shows SQL; format=false keeps the text verbatim (e.g. DDL with comments).
export function SqlBlock({ sql, maxHeight = 360, format = true }: { sql: string; maxHeight?: number; format?: boolean }) {
  const formatted = useMemo(() => (format ? formatSQL(sql) : sql), [sql, format]);
  return (
    <div style={{ position: 'relative' }}>
      <Typography.Paragraph copyable={{ text: sql }} style={{ position: 'absolute', right: 8, top: 4, margin: 0 }} />
      <pre className="sql-block" style={{ maxHeight }}>
        {formatted}
      </pre>
    </div>
  );
}

// Typical identities from fixtures/permission_cases.json.
const presets: {id:number;note:string}[] = [
 {id:175,note:'系统管理员，全部数据'},
 {id:161,note:'总部分管，3 家分公司'},
 {id:162,note:'总部分管，5 个指定营业部'},
 {id:151,note:'上海分公司主管'},
 {id:121,note:'第 1 营业部主管'},
 {id:1,note:'客户经理，含跨营业部服务'},
 {id:176,note:'离职员工，无客户权限'},
];

export function UserPicker({
  value,
  onChange,
  style,
  disabled,
  size,
}: {
  value: number;
  onChange: (id: number) => void;
  style?: React.CSSProperties;
  disabled?: boolean;
  size?: 'small' | 'middle' | 'large';
}) {
  const [users, setUsers] = useState<UserSummary[]>([]);
  useEffect(() => {
    api.users().then(setUsers).catch(() => setUsers([]));
  }, []);
  const label = (u: UserSummary) =>
    `${u.emp_no} ${u.name} · ${u.roles || '无角色'}${u.status !== 'active' ? ' · 已离职' : ''}`;
  const byId = new Map(users.map((u) => [u.id, u]));
  const options = [
    {
      label: '典型身份',
      options: presets.map((p) => ({
        value: p.id,
        label: byId.has(p.id) ? `${label(byId.get(p.id)!)}（${p.note}）` : `#${p.id}（${p.note}）`,
      })),
    },
    {
      label: '全部员工',
      options: users.filter((u) => !presets.some((p) => p.id === u.id)).map((u) => ({ value: u.id, label: label(u) })),
    },
  ];
  return (
    <Select
      disabled={disabled}
      size={size}
      showSearch={{ optionFilterProp: 'label' }}
      value={value}
      onChange={onChange}
      options={options}
      style={{ minWidth: 340, ...style }}
      popupMatchSelectWidth={false}
    />
  );
}

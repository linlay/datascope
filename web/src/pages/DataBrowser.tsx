import { useI18n } from '../ui/preferences';
import { useEffect, useMemo, useState } from 'react';
import { Alert, Card, Descriptions, Empty, Input, Segmented, Space, Table, Tag, Tooltip, Typography } from 'antd';
import type { ColumnsType, SorterResult } from 'antd/es/table/interface';
import { api, type BrowseResult, type Row, type TableInfo, type TableSchema } from '../api';
import { CellValue, SchemeTag, SqlBlock } from '../components/shared';

const { Text } = Typography;

interface Filter {
  col: string;
  val: string;
}

export function DataBrowser({ table, onOpenTable }: { table?: string; onOpenTable: (table: string, filter?: Filter) => void }) {
 const t=useI18n();
  const [tables, setTables] = useState<TableInfo[]>([]);
  const [error, setError] = useState<string>();
  const [search, setSearch] = useState('');

  useEffect(() => {
    api.tables().then(setTables).catch((e: Error) => setError(e.message));
  }, []);

  const groups = useMemo(() => {
    const term = search.trim().toLowerCase();
    const visible = tables.filter((tableInfo) => !term || tableInfo.name.toLowerCase().includes(term) || tableInfo.comment.toLowerCase().includes(term) || (tableInfo.display_name??'').toLowerCase().includes(term));
    return Array.from(new Set(tables.map(tableInfo=>tableInfo.scheme)))
      .map((scheme) => ({ scheme, tables: visible.filter((tableInfo) => tableInfo.scheme === scheme) }))
      .filter((g) => g.tables.length > 0);
  }, [tables, search]);

  const current = table ?? tables[0]?.name;

  return (
    <div className="browser">
      <Card size="small" className="browser-tables" title={t("数据表（{0}）", {"0":(tables.length)})}>
        <Input.Search placeholder={t("按表名或中文注释筛选")} allowClear onChange={(e) => setSearch(e.target.value)} style={{ marginBottom: 8 }} />
        {error && <Alert type="error" message={error} showIcon />}
        {groups.map((g) => (
          <div key={g.scheme} className="table-group">
            <div className="table-group-title">
              <SchemeTag scheme={g.scheme} />
            </div>
            {g.tables.map((tableInfo) => (
              <button
                key={tableInfo.name}
                type="button"
                className={`table-item${tableInfo.name === current ? ' active' : ''}`}
                onClick={() => onOpenTable(tableInfo.name)}
              >
                <span className="table-item-name mono">{tableInfo.name}</span>
                <span className="table-item-count mono">{tableInfo.row_count_estimated?t("约 "):''}{tableInfo.row_count}</span>
                <span className="table-item-comment">{tableInfo.display_name||tableInfo.comment}</span>
              </button>
            ))}
          </div>
        ))}
      </Card>
      <div className="browser-main">{current ? <TableView key={current} table={current} tables={tables} onOpenTable={onOpenTable} /> : <Empty />}</div>
    </div>
  );
}

function initialFilter(): Filter | undefined {
  const params = new URLSearchParams(window.location.hash.split('?')[1] ?? '');
  const col = params.get('col');
  return col ? { col, val: params.get('val') ?? '' } : undefined;
}

function TableView({
  table,
  tables,
  onOpenTable,
}: {
  table: string;
  tables: TableInfo[];
  onOpenTable: (table: string, filter?: Filter) => void;
}) {
 const t=useI18n();
  const info = tables.find((tableInfo) => tableInfo.name === table);
  const [schema, setSchema] = useState<TableSchema>();
  const [page, setPage] = useState<BrowseResult>();
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string>();
  const [query, setQuery] = useState({ limit: 20, offset: 0, order: '', desc: false, q: '', filter: initialFilter() });
  const [view, setView] = useState<'rows' | 'schema'>('rows');

  useEffect(() => {
    api.describe(table).then(setSchema).catch((e: Error) => setError(e.message));
  }, [table]);

  useEffect(() => {
    setLoading(true);
    api
      .rows(table, { ...query, col: query.filter?.col, val: query.filter?.val })
      .then((p) => {
        setPage(p);
        setError(undefined);
      })
      .catch((e: Error) => setError(e.message))
      .finally(() => setLoading(false));
  }, [table, query]);

  const columns: ColumnsType<Row> = (schema?.columns ?? []).map((c) => ({
    key: c.name,
    dataIndex: c.name,
    sorter: true,
    sortOrder: query.order === c.name ? (query.desc ? 'descend' : 'ascend') : null,
    title: (
      <Tooltip title={`${c.type}${c.references ? ` → ${c.references}` : ''}`}>
        <div className="col-title">
          <span className="mono">
            {c.primary_key && <Tag color="gold" style={{ marginRight: 4 }}>PK</Tag>}
            {c.name}
          </span>
          <span className="col-comment">{c.display_name?`${c.display_name} · ${c.comment}`:c.comment}</span>
        </div>
      </Tooltip>
    ),
    render: (value: unknown) => {
      if (c.references && value !== null && value !== undefined) {
        const [refTable, refColumn] = c.references.split('.');
        return (
          <a onClick={() => onOpenTable(refTable, { col: refColumn, val: String(value) })} title={t("查看 {0} = {1}", {"0":(c.references),"1":(value)})}>
            <CellValue column={c.name} value={value} />
          </a>
        );
      }
      return <CellValue column={c.name} value={value} />;
    },
  }));

  return (
    <Card
      size="small"
      title={
        <Space wrap>
          <span className="mono" style={{ fontSize: 16 }}>{table}</span>
          <Text type="secondary">{schema?.display_name?`${schema.display_name} · ${schema.comment}`:schema?.comment}</Text>
          {info && <SchemeTag scheme={info.scheme} />}
        </Space>
      }
      extra={
        <Segmented
          size="small"
          value={view}
          onChange={(v) => setView(v as 'rows' | 'schema')}
          options={[
            { label: t("数据"), value: 'rows' },
            { label: t("结构"), value: 'schema' },
          ]}
        />
      }
    >
      {view === 'rows' ? (
        <>
                <Space wrap style={{ marginBottom: 12 }}>
                  <Input.Search
                    placeholder={t("在所有字段中搜索")}
                    allowClear
                    enterButton
                    style={{ width: 300 }}
                    onSearch={(q) => setQuery((s) => ({ ...s, q, offset: 0 }))}
                  />
                  {query.filter && (
                    <Tag closable color="processing" onClose={() => setQuery((s) => ({ ...s, filter: undefined, offset: 0 }))}>
                      {query.filter.col} = {query.filter.val}
                    </Tag>
                  )}
                  <Text type="secondary">{t("超级管理员视图：全量数据，未经权限改写")}</Text>
                </Space>
                {error && <Alert type="error" message={error} showIcon style={{ marginBottom: 12 }} />}
                <Table<Row>
                  size="small"
                  bordered
                  loading={loading}
                  columns={columns}
                  dataSource={page?.rows ?? []}
                  rowKey={(row) => String(row[schema?.columns[0]?.name ?? 'id'])}
                  scroll={{ x: 'max-content' }}
                  pagination={{
                    current: Math.floor(query.offset / query.limit) + 1,
                    pageSize: query.limit,
                    total: page?.total ?? 0,
                    showSizeChanger: true,
                    pageSizeOptions: [20, 50, 100, 200, 500],
                    showTotal: (total, [from, to]) => t("{0}–{1} / 共 {2} 行", {"0":(from),"1":(to),"2":(total)}),
                  }}
                  onChange={(pagination, _filters, sorter) => {
                    const s = sorter as SorterResult<Row>;
                    const limit = pagination.pageSize ?? query.limit;
                    setQuery((q) => ({
                      ...q,
                      limit,
                      offset: ((pagination.current ?? 1) - 1) * limit,
                      order: s.order ? String(s.columnKey) : '',
                      desc: s.order === 'descend',
                    }));
                  }}
                />
        </>
      ) : (
        schema && (
          <Space orientation="vertical" style={{ width: '100%' }} size="middle">
            <Table
              size="small"
              bordered
              pagination={false}
              rowKey="name"
              dataSource={schema.columns}
              columns={[
                { title: t("字段"), dataIndex: 'name', render: (v: string) => <span className="mono">{v}</span> },
                { title: t("类型"), dataIndex: 'type', render: (v: string) => <span className="mono">{v}</span> },
                { title: t("主键"), dataIndex: 'primary_key', render: (v: boolean) => (v ? <Tag color="gold">PK</Tag> : null) },
                { title: t("非空"), dataIndex: 'not_null', render: (v: boolean) => (v ? t("是") : '') },
                {
                  title: t("引用"),
                  dataIndex: 'references',
                  render: (v?: string) => (v ? <a onClick={() => onOpenTable(v.split('.')[0])} className="mono">{v}</a> : null),
                },
                { title: t("中文说明"), dataIndex: 'comment' },
              ]}
            />
            <Descriptions size="small" column={1} bordered items={[{ key: 'idx', label: t("索引"), children: schema.indexes.join('、') || '无' }]} />
            <SqlBlock sql={schema.ddl} format={false} />
          </Space>
        )
      )}
    </Card>
  );
}

# 证券经纪业务数据字典

由 Go 生成器导出。全部为合成数据，价格与金额均为整数分；日期为 YYYY-MM-DD，交易时间使用中国时区 RFC3339。证券代码为格式演示，非真实行情。

## branches — 分公司（星澜证券为虚构券商）

默认数据量：10 行。

| 字段 | 类型 | 必填 | 主键 | 中文说明 |
| --- | --- | --- | --- | --- |
| code | TEXT | 是 | 是 | 业务代码 |
| name | TEXT | 是 |  | 名称 |
| city | TEXT | 是 |  | 所在城市 |
| address | TEXT | 是 |  | 机构地址（虚构门牌） |

## cash_flows — 资金流水（有符号净发生额，分）

默认数据量：123258 行。

| 字段 | 类型 | 必填 | 主键 | 中文说明 |
| --- | --- | --- | --- | --- |
| id | INTEGER | 是 | 是 | 内部主键 |
| customer_no | TEXT | 是 |  | 客户业务编号 |
| fund_account_no | TEXT | 是 |  | 资金账号 |
| trade_date | TEXT | 是 |  | 交易日期 YYYY-MM-DD |
| flow_type | TEXT | 是 |  | 流水类型 DEPOSIT/WITHDRAWAL/BUY/SELL |
| trade_id | INTEGER | 否 |  | 对应成交编号（转入转出为空） |
| amount_cents | INTEGER | 是 |  | 成交金额或资金净发生额（分，流水带正负号） |
| fee_cents | INTEGER | 是 |  | 费用合计（分） |
| balance_after | INTEGER | 是 |  | 流水后资金余额（分） |

## customers — 证券经纪客户（合成、脱敏）

默认数据量：1500 行。

| 字段 | 类型 | 必填 | 主键 | 中文说明 |
| --- | --- | --- | --- | --- |
| customer_no | TEXT | 是 | 是 | 客户业务编号 |
| customer_type | TEXT | 是 |  | 客户类型 INDIVIDUAL（个人）/CORPORATE（企业） |
| name | TEXT | 是 |  | 名称 |
| gender | TEXT | 是 |  | 性别 |
| birth_date | TEXT | 是 |  | 出生日期 |
| id_type | TEXT | 是 |  | 证件类型 |
| id_no | TEXT | 是 |  | 证件号码（预先脱敏） |
| mobile | TEXT | 是 |  | 手机号码（预先脱敏） |
| risk_level | TEXT | 是 |  | 客户风险等级 C1–C5 |
| investor_type | TEXT | 是 |  | 投资者类型（普通/专业） |
| open_date | TEXT | 是 |  | 开户日期 |
| status | TEXT | 是 |  | 状态（员工 active/resigned；账户 active） |

## daily_balances — 资金日结（分）

默认数据量：238769 行。

| 字段 | 类型 | 必填 | 主键 | 中文说明 |
| --- | --- | --- | --- | --- |
| id | INTEGER | 是 | 是 | 内部主键 |
| customer_no | TEXT | 是 |  | 客户业务编号 |
| fund_account_no | TEXT | 是 |  | 资金账号 |
| trade_date | TEXT | 是 |  | 交易日期 YYYY-MM-DD |
| opening_balance | INTEGER | 是 |  | 期初资金余额（分） |
| transfer_in | INTEGER | 是 |  | 当日转入金额（分） |
| transfer_out | INTEGER | 是 |  | 当日转出金额（分） |
| buy_amount | INTEGER | 是 |  | 买入成交总金额（分，不含费用） |
| sell_amount | INTEGER | 是 |  | 卖出成交总金额（分，不含费用） |
| fees | INTEGER | 是 |  | 当日交易费用（分） |
| closing_balance | INTEGER | 是 |  | 期末资金余额（分） |
| available_balance | INTEGER | 是 |  | 可用资金（分） |
| withdrawable_balance | INTEGER | 是 |  | 可取资金（分，保守扣除当日卖出金额） |

## daily_quotes — 模拟日行情（价格及金额为分）

默认数据量：6780 行。

| 字段 | 类型 | 必填 | 主键 | 中文说明 |
| --- | --- | --- | --- | --- |
| id | INTEGER | 是 | 是 | 内部主键 |
| security_code | TEXT | 是 |  | 证券代码，关联 securities.code |
| trade_date | TEXT | 是 |  | 交易日期 YYYY-MM-DD |
| prev_close | INTEGER | 是 |  | 前收盘价（分） |
| open | INTEGER | 是 |  | 开盘价（分） |
| high | INTEGER | 是 |  | 最高价（分） |
| low | INTEGER | 是 |  | 最低价（分） |
| close | INTEGER | 是 |  | 收盘价（分） |
| volume | INTEGER | 是 |  | 成交量（股，模拟） |
| amount | INTEGER | 是 |  | 行情成交金额（分，模拟） |

## departments — 开户营业部

默认数据量：30 行。

| 字段 | 类型 | 必填 | 主键 | 中文说明 |
| --- | --- | --- | --- | --- |
| code | TEXT | 是 | 是 | 业务代码 |
| branch_code | TEXT | 是 |  | 分公司代码，关联 branches.code |
| name | TEXT | 是 |  | 名称 |
| address | TEXT | 是 |  | 机构地址（虚构门牌） |

## employee_scopes — 员工管理范围（含生效日期）

默认数据量：66 行。

| 字段 | 类型 | 必填 | 主键 | 中文说明 |
| --- | --- | --- | --- | --- |
| id | INTEGER | 是 | 是 | 内部主键 |
| employee_id | INTEGER | 是 |  | 员工编号（employees 主键，非自增） |
| branch_code | TEXT | 否 |  | 分公司代码，关联 branches.code |
| department_code | TEXT | 否 |  | 营业部代码，关联 departments.code |
| effective_date | TEXT | 是 |  | 生效日期（含当天） |
| expiry_date | TEXT | 否 |  | 失效日期（不含当天），空为长期 |

## employees — 员工登录身份

默认数据量：100 行。

| 字段 | 类型 | 必填 | 主键 | 中文说明 |
| --- | --- | --- | --- | --- |
| employee_id | INTEGER | 是 | 是 | 员工编号（employees 主键，非自增） |
| emp_no | TEXT | 是 |  | 员工工号（OAuth sub） |
| name | TEXT | 是 |  | 名称 |
| role_id | INTEGER | 是 |  | 岗位角色编号 |
| branch_code | TEXT | 否 |  | 分公司代码，关联 branches.code |
| department_code | TEXT | 否 |  | 营业部代码，关联 departments.code |
| title | TEXT | 是 |  | 职务 |
| mobile | TEXT | 是 |  | 手机号码（预先脱敏） |
| hire_date | TEXT | 是 |  | 入职日期 |
| status | TEXT | 是 |  | 状态（员工 active/resigned；账户 active） |

## fund_accounts — 资金账户

默认数据量：2113 行。

| 字段 | 类型 | 必填 | 主键 | 中文说明 |
| --- | --- | --- | --- | --- |
| fund_account_no | TEXT | 是 | 是 | 资金账号 |
| customer_no | TEXT | 是 |  | 客户业务编号 |
| department_code | TEXT | 是 |  | 营业部代码，关联 departments.code |
| manager_id | INTEGER | 是 |  | 服务客户经理编号（允许跨营业部） |
| currency | TEXT | 是 |  | 结算币种 CNY |
| status | TEXT | 是 |  | 状态（员工 active/resigned；账户 active） |

## orders — 证券交易委托

默认数据量：167162 行。

| 字段 | 类型 | 必填 | 主键 | 中文说明 |
| --- | --- | --- | --- | --- |
| id | INTEGER | 是 | 是 | 内部主键 |
| order_no | TEXT | 是 |  | 委托业务编号 |
| trade_date | TEXT | 是 |  | 交易日期 YYYY-MM-DD |
| order_time | TEXT | 是 |  | 委托时间（中国时区 RFC3339） |
| customer_no | TEXT | 是 |  | 客户业务编号 |
| fund_account_no | TEXT | 是 |  | 资金账号 |
| stock_account_id | INTEGER | 是 |  | 证券账户编号 |
| security_code | TEXT | 是 |  | 证券代码，关联 securities.code |
| side | TEXT | 是 |  | 买卖方向 BUY/SELL |
| price | INTEGER | 是 |  | 委托或成交价格（分） |
| quantity | INTEGER | 是 |  | 数量（股） |
| filled_quantity | INTEGER | 是 |  | 委托累计成交股数 |
| status | TEXT | 是 |  | 状态（员工 active/resigned；账户 active） |
| channel | TEXT | 是 |  | 渠道 APP/PC/COUNTER |

## positions — 日终持仓（T+1 可卖数量）

默认数据量：1129732 行。

| 字段 | 类型 | 必填 | 主键 | 中文说明 |
| --- | --- | --- | --- | --- |
| id | INTEGER | 是 | 是 | 内部主键 |
| customer_no | TEXT | 是 |  | 客户业务编号 |
| fund_account_no | TEXT | 是 |  | 资金账号 |
| security_code | TEXT | 是 |  | 证券代码，关联 securities.code |
| trade_date | TEXT | 是 |  | 交易日期 YYYY-MM-DD |
| opening_quantity | INTEGER | 是 |  | 期初持仓股数 |
| buy_quantity | INTEGER | 是 |  | 当日买入股数 |
| sell_quantity | INTEGER | 是 |  | 当日卖出股数（不得超过期初） |
| quantity | INTEGER | 是 |  | 数量（股） |
| available_quantity | INTEGER | 是 |  | 当日收盘可卖股数（扣除当日买入） |
| cost_cents | INTEGER | 是 |  | 剩余持仓成本（分，含买入费用） |
| close_price | INTEGER | 是 |  | 当日收盘价（分） |
| market_value_cents | INTEGER | 是 |  | 持仓市值（分） |

## roles — 岗位角色

默认数据量：5 行。

| 字段 | 类型 | 必填 | 主键 | 中文说明 |
| --- | --- | --- | --- | --- |
| id | INTEGER | 是 | 是 | 内部主键 |
| code | TEXT | 是 |  | 业务代码 |
| name | TEXT | 是 |  | 名称 |
| description | TEXT | 是 |  | 角色职责说明 |

## schema_comments — 中文数据字典

默认数据量：154 行。

| 字段 | 类型 | 必填 | 主键 | 中文说明 |
| --- | --- | --- | --- | --- |
| table_name | TEXT | 是 | 是 | 表名 |
| column_name | TEXT | 是 | 是 | 字段名（空为表注释） |
| comment_zh | TEXT | 是 |  | 中文注释 |

## securities — 模拟 A 股证券目录

默认数据量：60 行。

| 字段 | 类型 | 必填 | 主键 | 中文说明 |
| --- | --- | --- | --- | --- |
| code | TEXT | 是 | 是 | 证券代码（主键，六位字符串） |
| name | TEXT | 是 |  | 名称 |
| exchange | TEXT | 是 |  | 交易所 SH/SZ |
| board | TEXT | 是 |  | 板块 MAIN/STAR |
| security_type | TEXT | 是 |  | 证券类型 A_SHARE |
| lot_size | INTEGER | 是 |  | 委托数量单位（股） |
| min_buy_quantity | INTEGER | 是 |  | 最低买入委托股数 |

## stock_accounts — 沪深证券账户

默认数据量：4226 行。

| 字段 | 类型 | 必填 | 主键 | 中文说明 |
| --- | --- | --- | --- | --- |
| id | INTEGER | 是 | 是 | 内部主键 |
| fund_account_no | TEXT | 是 |  | 资金账号 |
| exchange | TEXT | 是 |  | 交易所 SH/SZ |
| stock_account_no | TEXT | 是 |  | 证券账号（沪 A+9 位，深 10 位） |

## trades — 证券成交及费用（分）

默认数据量：122580 行。

| 字段 | 类型 | 必填 | 主键 | 中文说明 |
| --- | --- | --- | --- | --- |
| id | INTEGER | 是 | 是 | 内部主键 |
| trade_no | TEXT | 是 |  | 成交业务编号 |
| order_id | INTEGER | 是 |  | 所属委托编号 |
| customer_no | TEXT | 是 |  | 客户业务编号 |
| fund_account_no | TEXT | 是 |  | 资金账号 |
| security_code | TEXT | 是 |  | 证券代码，关联 securities.code |
| trade_date | TEXT | 是 |  | 交易日期 YYYY-MM-DD |
| trade_time | TEXT | 是 |  | 成交时间（中国时区 RFC3339） |
| side | TEXT | 是 |  | 买卖方向 BUY/SELL |
| price | INTEGER | 是 |  | 委托或成交价格（分） |
| quantity | INTEGER | 是 |  | 数量（股） |
| amount_cents | INTEGER | 是 |  | 成交金额或资金净发生额（分，流水带正负号） |
| commission_cents | INTEGER | 是 |  | 佣金（分，按委托计费后分摊） |
| stamp_tax_cents | INTEGER | 是 |  | 卖出印花税（分） |
| transfer_fee_cents | INTEGER | 是 |  | 过户费（分） |
| fee_cents | INTEGER | 是 |  | 费用合计（分） |

## trading_calendar — 2026 交易日历（上交所休市安排）

默认数据量：168 行。

| 字段 | 类型 | 必填 | 主键 | 中文说明 |
| --- | --- | --- | --- | --- |
| trade_date | TEXT | 是 | 是 | 交易日期 YYYY-MM-DD |
| is_open | INTEGER | 是 |  | 是否开市（1 是，0 否） |

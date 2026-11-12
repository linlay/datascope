# 分地区销售演示库

全部为虚构的家电及数码销售数据，不连接真实客户、银行或订单系统。
默认账期为 2024-11-01 至 2026-10-31（24个完整月份），金额为人民币整数分。


## 本地生成与安装

生成数据只需要 Python 3 标准库；也可直接使用生成后的 SQLite 文件。配置使用平台通用导入器，无需编译本例专用代码。

```sh
make init-regional-sales
# 在 .env 中设置 DATASCOPE_EXAMPLE_PASSWORD（至少12字符）后：
make check-example EXAMPLE=regional-sales
make import-example EXAMPLE=regional-sales
```

生成文件为 `data/regional_sales_demo.sqlite3`，脚本拒绝覆盖已有文件。
客户采用带地域和行业的企业名称，员工和联系人使用中文姓名，商品包含品牌、规格和型号。演示数据说明集中放在本文和库说明中，业务记录不带“虚构客户”“演示型号”等占位标签。

已有本地库可显式刷新展示数据：

```sh
python3 examples/regional-sales/generate.py --refresh-labels
```

刷新前通过 SQLite Backup 保存一致性备份到 `data/legacy/`。刷新只更新企业、人员、商品、组织和价目表名称及联系人信息；提交前校验其余字段摘要一致、外键和数据库完整性。主键、业务金额、时间、订单归属及员工权限范围保持原值。联系邮箱使用保留的 `.example` 域名。

`bundle.yml`、`rewrite.yml`、`sql-examples.yml` 分别保存数据库与账号清单、静态权限 SQL 和10条工作台查询。通用导入器注册数据源、保存配置、发布知识并创建6个业务账号。
安装前会在所有6个业务身份下执行示例，验证失败不会注册数据源。
重复安装保留现有账号密码、停用状态、权限和自定义配置。
数据库文件和本地管理配置不纳入Git，DDL、配置模板、Python 造数脚本及文档用于重建。

小规模或其他账期的独立数据文件可用：

```sh
python3 examples/regional-sales/generate.py --db /tmp/regional-small.sqlite3 --orders 1200 --seed 20261111 --end-month 2026-10
```

默认固定随机种子，可重复生成；至少240张订单，覆盖24个月。
自选账期后需同步调整SQL示例中的账龄/RFM固定日期。

登录现有管理控制台后选择“分地区销售演示库”，在查询工作台的SQL示例中选取统计查询。
平台管理员可模拟下表中的业务员工ID检查数据范围；测试真实员工认证时使用本库MCP入口。
业务密码从环境变量读取，源代码、日志和文档不保存密码原文。

## 关系与数据字典

- 地理：regions → provinces → cities；sales_offices 关联大区和城市。
- 组织：sales_offices → departments → employees；employee_scopes 维护 ALL / REGION / OFFICE / SELF 授权。
- 客户：customers 关联归属地区、分公司、销售员、渠道、城市；customer_contacts 关联客户。
- 商品：product_categories → products；price_lists → product_prices 维护地区价格。
- 仓储：warehouses → inventory_balances，库存为 2026-10-31 快照，不是进销存流水账，不用于历史库存周转或缺货推断。
- 订单：sales_orders → sales_order_items；订单记录成交时的地区、公司、销售员、渠道；明细保存成交价格、成本和整行折扣快照。
- 履约：shipments → shipment_items → sales_order_items，支持一笔订单分批发货。
- 售后：sales_returns → sales_return_items → sales_order_items，支持部分退货。
- 回款：payments → payment_allocations → sales_orders，支持分次付款；refunds 同时关联退货单和原回款。
- 考核：sales_targets 每行是一个销售员一个月的目标，按销售员汇总到公司或地区，不能把同一目标跨订单重复求和。
- 元数据：schema_comments 保存所有表及字段的中文注释，表结构工具可读出字段、外键、索引及说明。

## 统计口径

1. 默认销售业绩排除 pending、cancelled；confirmed、partial_shipped、shipped 计入下单业绩，不能称为财务收入确认。
2. sales_orders.total_cents = 所有订单明细 net_cents 之和；net_cents = quantity × unit_price_cents − discount_cents。金额不另计税费或运费。
3. 商品毛利 = net_cents − quantity × unit_cost_cents；采用历史成交成本，不使用当前标准成本重算历史。
4. 只有 received 退货减少净销售；requested、approved、rejected 不减少销售。退货数量不超过已发货数量，退货金额按折后明细单位金额取整。
5. 回款按 payment_allocations 核销到订单；净回款 = 核销回款 − 实际退款。应收净额 = 订单总额 − 已收货退货金额 − 核销回款 + 已退款。负值表示待退客户款，不能直接截断为零。
6. 示例04按订单归属汇总当前净额，示例09按销售/退货各自发生月份汇总事件；两种月度口径不同。
7. 多个一对多分支（订单明细、发货、退货、回款）先分别聚合到订单或订单明细粒度，再 JOIN；直接一起关联会重复累计。
8. 同比和环比示例使用 LAG(12/1)，仅适用于连续月份有业务的分组；如需展示无销售月份，应构造月份日历补零再计算。窄权限或自选时间范围时须注意缺月。
9. 账龄/RFM 固定截至 2026-10-31；重新生成其他账期时，按实际快照日期调整查询中的固定日期。
10. 发货与回款日期在账期末截止；退货只来自已发货订单，退款只来自已收到退货的原回款。

## 权限与登录

独立业务账号，与平台管理账号及证券库账号完全隔离。

| 账号 | 业务员工ID | 范围 |
|---|---:|---|
| S0001 | 1 | 总部，全国 |
| S0002 | 2 | 华东大区 |
| S0003 | 3 | 华南大区 |
| S0009 | 9 | 上海中心分公司 |
| 107078 | 107078 | 上海中心销售员甲，仅本人的客户/订单/回款/目标 |
| 900330 | 900330 | 同公司销售员乙，仅本人的客户/订单/回款/目标 |

新账号初始密码由导入时 DATASCOPE_EXAMPLE_PASSWORD 提供。重复导入保留同目标已有库的全部配置，包括 OneID/SSO、账号密码、停用状态和自定义策略。

- 地区、公司权限从 employee_scopes 读取；销售员按单据历史归属过滤，不按客户当前经理重算历史归属。
- 所有订单、发货、退货、回款、退款明细表独立配置权限，单表、子查询、CTE、UNION 均须经过 query 改写。
- 库存按可见公司/地区开放；价目表按可见地区开放；商品和地理字典全体在职员工可读。
- 联系人 phone、email 不向业务查询开放；SELECT * 也不返回这两个字段。
- 没有 raw_query、action 权限，不启用写入。总部业务账号仍受字段规则约束。平台管理员 raw_query 为独立管理能力，不能用于演示业务行权限。
- MCP 路径为 /mcp/regional_sales_demo。首次安装创建独立员工账号；已安装库可在管理控制台配置 OneID、SSO 等认证方式，重复安装保留当前配置。

## 复杂 SQL

工作台内置10个示例，覆盖多层CTE、窗口聚合、LAG、DENSE_RANK、NTILE、帕累托累计占比、相关子查询、UNION ALL、条件聚合与HAVING。
这是SQLite查询子集，不承诺所有方言和任意SQL都支持。当前平台明确不支持递归CTE；写语句也不通过query开放。

## 表规模与字段字典


### cities — 城市（40 行）

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | INTEGER | 主键编号 |
| `province_id` | INTEGER | 省份编号 |
| `name` | TEXT | 名称 |

### customer_contacts — 客户联系人（3,000 行）

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | INTEGER | 主键编号 |
| `customer_id` | INTEGER | 客户编号 |
| `name` | TEXT | 名称 |
| `phone` | TEXT | 联系电话（不向业务查询开放） |
| `email` | TEXT | 联系邮箱（不向业务查询开放） |
| `job_title` | TEXT | 联系人职务 |

### customers — 客户（3,000 行）

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | INTEGER | 主键编号 |
| `customer_no` | TEXT | 客户编码 |
| `name` | TEXT | 名称 |
| `city_id` | INTEGER | 城市编号 |
| `region_id` | INTEGER | 销售大区编号 |
| `office_id` | INTEGER | 分公司编号 |
| `owner_employee_id` | INTEGER | 归属销售员编号 |
| `channel_id` | INTEGER | 销售渠道编号 |
| `segment` | TEXT | 客户分层 |
| `acquired_date` | TEXT | 客户建立日期 |

### departments — 部门（30 行）

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | INTEGER | 主键编号 |
| `office_id` | INTEGER | 分公司编号 |
| `name` | TEXT | 名称 |

### employee_scopes — 员工数据范围（300 行）

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | INTEGER | 主键编号 |
| `employee_id` | INTEGER | 员工编号 |
| `scope_type` | TEXT | 授权范围类型 |
| `region_id` | INTEGER | 销售大区编号 |
| `office_id` | INTEGER | 分公司编号 |

### employees — 员工（300 行）

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | INTEGER | 主键编号 |
| `employee_no` | TEXT | 工号 |
| `name` | TEXT | 名称 |
| `department_id` | INTEGER | 部门编号 |
| `office_id` | INTEGER | 分公司编号 |
| `region_id` | INTEGER | 销售大区编号 |
| `role` | TEXT | 业务岗位 |
| `active` | INTEGER | 是否在职或启用 |

### inventory_balances — 库存快照（9,000 行）

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | INTEGER | 主键编号 |
| `warehouse_id` | INTEGER | 仓库编号 |
| `product_id` | INTEGER | 商品编号 |
| `on_hand` | INTEGER | 库存数量 |
| `reserved` | INTEGER | 预留库存数量 |
| `snapshot_date` | TEXT | 库存快照日期 |

### payment_allocations — 回款核销（58,704 行）

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | INTEGER | 主键编号 |
| `payment_id` | INTEGER | 回款编号 |
| `order_id` | INTEGER | 订单编号 |
| `amount_cents` | INTEGER | 业务金额（分） |

### payments — 回款（58,704 行）

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | INTEGER | 主键编号 |
| `customer_id` | INTEGER | 客户编号 |
| `region_id` | INTEGER | 销售大区编号 |
| `office_id` | INTEGER | 分公司编号 |
| `owner_employee_id` | INTEGER | 归属销售员编号 |
| `paid_date` | TEXT | 回款日期 |
| `method` | TEXT | 支付方式 |
| `amount_cents` | INTEGER | 业务金额（分） |

### price_lists — 地区价目表（7 行）

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | INTEGER | 主键编号 |
| `region_id` | INTEGER | 销售大区编号 |
| `name` | TEXT | 名称 |
| `valid_from` | TEXT | 有效期开始 |
| `valid_to` | TEXT | 有效期结束 |

### product_categories — 商品分类（10 行）

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | INTEGER | 主键编号 |
| `name` | TEXT | 名称 |

### product_prices — 商品价格（2,100 行）

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | INTEGER | 主键编号 |
| `price_list_id` | INTEGER | 价目表编号 |
| `product_id` | INTEGER | 商品编号 |
| `price_cents` | INTEGER | 地区商品价格（分） |

### products — 商品（300 行）

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | INTEGER | 主键编号 |
| `sku` | TEXT | 商品编码 |
| `name` | TEXT | 名称 |
| `category_id` | INTEGER | 商品分类编号 |
| `brand` | TEXT | 品牌 |
| `list_price_cents` | INTEGER | 商品目录价格（分） |
| `standard_cost_cents` | INTEGER | 标准成本（分） |
| `active` | INTEGER | 是否在职或启用 |

### provinces — 省份（20 行）

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | INTEGER | 主键编号 |
| `region_id` | INTEGER | 销售大区编号 |
| `name` | TEXT | 名称 |

### refunds — 退款（1,147 行）

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | INTEGER | 主键编号 |
| `return_id` | INTEGER | 退货单编号 |
| `payment_id` | INTEGER | 回款编号 |
| `refund_date` | TEXT | 退款日期 |
| `amount_cents` | INTEGER | 业务金额（分） |

### regions — 销售大区（7 行）

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | INTEGER | 主键编号 |
| `code` | TEXT | 代码 |
| `name` | TEXT | 名称 |

### sales_channels — 销售渠道（4 行）

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | INTEGER | 主键编号 |
| `code` | TEXT | 代码 |
| `name` | TEXT | 名称 |

### sales_offices — 销售分公司（30 行）

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | INTEGER | 主键编号 |
| `region_id` | INTEGER | 销售大区编号 |
| `city_id` | INTEGER | 城市编号 |
| `name` | TEXT | 名称 |

### sales_order_items — 订单明细（150,000 行）

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | INTEGER | 主键编号 |
| `order_id` | INTEGER | 订单编号 |
| `product_id` | INTEGER | 商品编号 |
| `quantity` | INTEGER | 数量 |
| `unit_price_cents` | INTEGER | 成交单价快照（分） |
| `unit_cost_cents` | INTEGER | 成交单位成本快照（分） |
| `discount_cents` | INTEGER | 整行折扣金额（分） |
| `net_cents` | INTEGER | 整行折后成交金额（分） |

### sales_orders — 销售订单（50,000 行）

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | INTEGER | 主键编号 |
| `order_no` | TEXT | 订单编码 |
| `customer_id` | INTEGER | 客户编号 |
| `region_id` | INTEGER | 销售大区编号 |
| `office_id` | INTEGER | 分公司编号 |
| `owner_employee_id` | INTEGER | 归属销售员编号 |
| `channel_id` | INTEGER | 销售渠道编号 |
| `order_date` | TEXT | 订单日期 |
| `due_date` | TEXT | 应收款到期日期 |
| `status` | TEXT | 业务状态 |
| `total_cents` | INTEGER | 订单折后总额（分，不含运费税额） |

### sales_return_items — 退货明细（2,906 行）

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | INTEGER | 主键编号 |
| `return_id` | INTEGER | 退货单编号 |
| `order_item_id` | INTEGER | 订单明细编号 |
| `quantity` | INTEGER | 数量 |
| `amount_cents` | INTEGER | 业务金额（分） |

### sales_returns — 退货单（2,906 行）

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | INTEGER | 主键编号 |
| `order_id` | INTEGER | 订单编号 |
| `return_date` | TEXT | 退货日期 |
| `reason` | TEXT | 退货原因 |
| `status` | TEXT | 业务状态 |

### sales_targets — 月度销售目标（6,288 行）

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | INTEGER | 主键编号 |
| `month` | TEXT | 目标月份（YYYY-MM） |
| `region_id` | INTEGER | 销售大区编号 |
| `office_id` | INTEGER | 分公司编号 |
| `owner_employee_id` | INTEGER | 归属销售员编号 |
| `target_cents` | INTEGER | 销售员月度目标金额（分） |

### schema_comments — 结构注释（168 行）

| 字段 | 类型 | 说明 |
|---|---|---|
| `table_name` | TEXT | 物理表名 |
| `column_name` | TEXT | 物理字段名 |
| `comment_zh` | TEXT | 中文说明 |

### shipment_items — 发货明细（190,046 行）

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | INTEGER | 主键编号 |
| `shipment_id` | INTEGER | 发货单编号 |
| `order_item_id` | INTEGER | 订单明细编号 |
| `quantity` | INTEGER | 数量 |

### shipments — 发货单（74,752 行）

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | INTEGER | 主键编号 |
| `order_id` | INTEGER | 订单编号 |
| `warehouse_id` | INTEGER | 仓库编号 |
| `shipped_date` | TEXT | 发货日期 |
| `delivered_date` | TEXT | 签收日期 |

### warehouses — 仓库（30 行）

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | INTEGER | 主键编号 |
| `region_id` | INTEGER | 销售大区编号 |
| `office_id` | INTEGER | 分公司编号 |
| `name` | TEXT | 名称 |

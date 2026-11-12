# 分地区销售演示库

全部为虚构的家电及数码销售数据，不连接真实客户、银行或订单系统。
默认账期为 2024-11-01 至 2026-10-31（24个完整月份），金额为人民币整数分。

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

新账号初始密码由通用导入器的 DATASCOPE_EXAMPLE_PASSWORD 环境变量提供。重复导入保留已有认证、账号和权限。

- 地区、公司权限从 employee_scopes 读取；销售员按单据历史归属过滤，不按客户当前经理重算历史归属。
- 所有订单、发货、退货、回款、退款明细表独立配置权限，单表、子查询、CTE、UNION 均须经过 query 改写。
- 库存按可见公司/地区开放；价目表按可见地区开放；商品和地理字典全体在职员工可读。
- 联系人 phone、email 不向业务查询开放；SELECT * 也不返回这两个字段。
- 没有 raw_query、action 权限，不启用写入。总部业务账号仍受字段规则约束。平台管理员 raw_query 为独立管理能力，不能用于演示业务行权限。
- MCP 路径为 /mcp/regional_sales_demo，使用该库当前配置的业务认证方式；可选择本库账号、OneID 或 SSO。

## 复杂 SQL

工作台内置10个示例，覆盖多层CTE、窗口聚合、LAG、DENSE_RANK、NTILE、帕累托累计占比、相关子查询、UNION ALL、条件聚合与HAVING。
这是SQLite查询子集，不承诺所有方言和任意SQL都支持。当前平台明确不支持递归CTE；写语句也不通过query开放。

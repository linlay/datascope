# 商店数据示例

在仓库根目录运行 `make init-shop`，使用 Python 标准库生成 `data/shop_demo.sqlite3`：**200 个客户、100 件商品、3,000 张订单**，保持现有三张表及字段不变。默认随机种子为 20261113；同样输入可重复生成相同数据，已有数据库拒绝覆盖。

`schema.sqlite.sql` 仍包含最小种子数据（4 个客户、6 件商品、8 张订单），可直接用 SQLite 执行 SQL；`generate.py` 保留这些记录并补齐规模。订单覆盖 2026 年 5 月至 10 月、pending/paid/shipped/cancelled 四种状态，客户分属业务身份 1、2、3、4。保留无订单客户 191–200、无销量商品 96–100，以及部分零库存商品。金额均为整数分，订单单价是成交时的快照；已付款及已发货订单计入销售额，待付款和取消订单不计入。

```sh
# 创建新库；--db 可指定其他输出，--seed 可指定随机种子。
python3 examples/shop/generate.py --db data/shop_demo.sqlite3
# 显式扩充已有样例：先备份到 data/backups/，再追加，保留原有记录。
python3 examples/shop/generate.py --db data/shop_demo.sqlite3 --expand-existing
```

扩充只适用于本示例结构和 ID 范围；已有数据超过目标范围时拒绝执行。缺失记录在同一事务中补齐，失败回滚，重复扩充不修改已有记录。执行时避免同时运行业务写入，备份权限为 0600 且不纳入 Git。

`rewrite.yml` 按可信业务身份过滤客户和订单，商品目录隐藏成本字段。
`bundle.yml` 提供可选的一次性声明式导入，账号 SHOP001 对应业务身份 1。
设置 `DATASCOPE_EXAMPLE_PASSWORD`（至少 12 字符）后运行：

```sh
make check-example EXAMPLE=shop
make import-example EXAMPLE=shop
```

也可以通过管理控制台添加数据库和配置权限。已有库的认证和账号不会被重复导入覆盖。

`sql-examples.yml` 提供月度销售额、客户销售排行、商品销量排行、订单状态分布、暂无订单的客户及零库存商品六个查询示例。新库通过配置包导入；已有库可将这些 SQL 复制到「查询工作台」执行，重复导入不会覆盖已有自定义配置。业务账号 SHOP001 只看到身份 1 的客户和订单；测试其他归属时在查询工作台模拟相应业务身份，或显式创建对应业务账号。商品全员可读，`cost_cents` 始终隐藏。

# 商店数据示例

`schema.sqlite.sql` 同时包含建表和虚构种子数据：4 个客户、6 件商品、8 张订单。
在仓库根目录运行 `make init-shop`，生成 `data/shop_demo.sqlite3`，也可直接用 SQLite 执行 SQL。
生成工具只依赖 Python 标准库，拒绝覆盖已有数据库。

`rewrite.yml` 按可信业务身份过滤客户和订单，商品目录隐藏成本字段。
`bundle.yml` 提供可选的一次性声明式导入，账号 SHOP001 对应业务身份 1。
设置 `DATASCOPE_EXAMPLE_PASSWORD`（至少 12 字符）后运行：

```sh
make check-example EXAMPLE=shop
make import-example EXAMPLE=shop
```

也可以通过管理控制台添加数据库和配置权限。已有库的认证和账号不会被重复导入覆盖。

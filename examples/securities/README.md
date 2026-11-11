# 证券数据示例

`schema.sqlite.sql` 为建表 SQL，`seed.sql` 为小规模虚构证券业务数据。
在仓库根目录执行 `make init`，或使用 Python 标准库工具：

```sh
python3 examples/create_sqlite.py --db data/securities_demo.sqlite3 examples/securities/schema.sqlite.sql examples/securities/seed.sql
```

也可用 SQLite 客户端依次执行两份 SQL。输出文件已存在时拒绝覆盖。

- `rewrite.yml`：完整的表、字段和行权限 SQL。
- `bundle.yml`：通用导入器的声明式数据库、认证与账号清单。
- `actions.yml`：18 个需要本人确认的业务 Action 和公共库。
- `views.yml`：可直接保存的完整 HTML 视图。
- `sql-examples.yml`：工作台查询示例。
- `docs/`：数据字典、业务权限和操作案例。

先在环境变量设置 `DATASCOPE_EXAMPLE_PASSWORD`（至少 12 字符），再执行：

```sh
make check-example EXAMPLE=securities
make import-example EXAMPLE=securities
```

新库示例账号为 E0001、E0121、E0175，分别对应员工 1、121、175。
配置包显式启用模拟 Action 写入，所有动作需要本人确认；仅针对虚构数据。
已登记库会保留现有账号、认证、权限和自定义配置，不会重新安装或改成示例认证。
测试用的大规模生成器和权限参考算法位于 `tests/`，用户使用这些 SQL 无需 Go 示例代码。

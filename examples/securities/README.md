# 证券演示数据库

面向使用者的描述和使用说明维护在 `bundle.yml` 中，导入后可在控制台点击“使用说明”阅读。正文提供自然语言提问、连续追问、角色视角、业务操作与结果解读；本 README 保留创建、维护和回归说明。

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

## 缩减已有数据库

默认模式保留全部客户、账户、组织、授权、行情、委托、成交及资金流水，只缩减 `positions` 和 `daily_balances` 的历史快照：

```sh
python3 examples/securities/compact.py --db data/securities_demo.sqlite3 --keep-days 20
```

若希望将已有约 100 MiB 的本地样例进一步减到约 50 MB，显式增加交易流水裁剪，并保留关键回归账户的完整委托、成交及资金流水：

```sh
python3 examples/securities/compact.py --db data/securities_demo.sqlite3 --keep-days 20 --trim-ledgers --preserve-account 880000001186
```

此模式将 `orders`、`trades`、`cash_flows` 也裁剪到与快照相同的起始交易日。客户与账户不抽样，组织、员工、授权、行情、表结构和原有索引全部保留。`--preserve-account` 可重复指定，必须与 `--trim-ledgers` 一起使用，未知账户会被拒绝；它保留指定资金账户的完整交易流水，不保留额外历史快照。本地账户 `880000001186` 包含撤单和渠道修正案例使用的历史委托 `O0000002309`，保留整组流水可维持关联与账务连续性，不只补一条委托。

脚本先取得写锁，再使用 SQLite 一致性备份保存原库到 `data/backups/`；备份权限为 0600，不纳入 Git。删除按资金流水、成交、委托及快照的依赖顺序在同一事务内完成。交易裁剪模式提交前检查外键及 15 项账务一致性，失败回滚全部删除；提交后执行 `VACUUM` 回收空间，保持原数据库文件路径不变。若空间整理失败，已提交的删除不会自动恢复，备份仍可用于恢复。执行时避免同时运行业务写入。

`--backup-dir` 可指定备份目录；`--keep-days` 必须为正整数，窗口按库内最新资金日结日期计算，不按电脑当前日期计算。原库不足指定天数时保留全部现有快照；只有快照模式不裁剪交易流水。脚本输出 JSON，包括备份路径、窗口、逐表删除行数、保留账户、账务检查数量及整理前后字节数。

完整样例只缩减快照时由 260.6 MiB 降至 100.1 MiB；进一步裁剪交易流水并保留上述回归账户后为 **47.32 MiB（49.62 MB）**。本地窗口为 2026-09-15 至 2026-10-20 的连续 20 个交易日，仍有 1,500 个客户、2,113 个资金账户、214,235 条持仓及 42,260 条资金日结。实际大小与范围随输入库变化。

保留窗口内的原记录和日初余额不重新计算；第一天的 opening_balance 与 opening_quantity 承接窗口前的期末余额。更早历史应查备份，缺失记录不代表余额或成交额为零。由于少量回归账户保留较早流水，各账户的历史覆盖长度可能不同，不能无日期条件比较累计成交额。`sql-examples.yml` 及配置包说明中的成交明细、成交总额和客户排行统一限定为 2026-09-15 至 2026-10-20；更换数据窗口时应同步调整 SQL 日期。全历史核账和压测仍使用完整备份；恢复前停止访问该库的服务，再用备份恢复原路径。

`make init` 仍只创建 8 个客户的微型 SQL 样例，不会自动重建或缩减已有大库。脚本不修改平台中的认证、账号或管理配置。新库可通过通用配置包导入；已有库的查询示例和使用说明需显式更新，重复导入保留自定义配置。权限和 Action 的隔离回归及本地快照验收方法见 `tests/README.md`。

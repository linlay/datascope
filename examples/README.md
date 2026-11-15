# 演示数据库

本目录只保存 SQL、声明式配置、业务说明及可选的 Python 造数脚本，不包含 Go 包、Go 安装器或服务启动逻辑。

当前本地保留以下四个演示数据库。各配置包的“描述”概括适合处理的业务问题，“使用说明”提供自然语言提问、追问路线、角色视角及操作步骤。通过已连接本库的 AI 客户端提问；使用说明不要求读者先了解 SQL 或表结构。技术查询示例与建库、权限和回归资料分别维护。

| 示例 | 数据来源 | 创建数据库 |
| --- | --- | --- |
| [证券演示数据库](securities/README.md) | `schema.sqlite.sql` + `seed.sql`，小规模虚构数据 | `make init` |
| [区域销售演示数据库](regional-sales/README.md) | DDL + Python 标准库造数脚本 | `make init-regional-sales` |
| [高校教务演示数据库](academic/README.md) | 24 张表、两个学期、1,000 学生、80 教师，含课表、选课、成绩、考勤与考试 | `make init-academic` |
| [渠道与社交媒体运营演示数据库](channel-operations/README.md) | 25 张表、4 个模拟平台、12 个账号、600 个选题、1,600 次发布，含指标修订和转化归因 | `make init-channel-operations` |

实际 SQLite 文件生成到 `data/`，拒绝覆盖已有文件；也可以用 SQLite 客户端直接执行 SQL。
证券示例提供小规模可重建数据，完整大规模数据只用于自动测试，不把数百 MB 数据库放入源码仓库。

已有大证券库可显式运行 `python3 examples/securities/compact.py --db data/securities_demo.sqlite3 --keep-days 20`，先备份，再只保留最近 20 个交易日的持仓和资金日结快照，默认保留全部交易流水。增加 `--trim-ledgers --preserve-account 880000001186` 可同步裁剪旧交易流水、保留关键回归账户历史，本地实测约 47.32 MiB；客户、账户及权限关系保留，统计示例按明确日期窗口计算。脚本只操作指定的本地业务库，备份位于 `data/backups/`，不修改账号和管理配置。

[商店](shop/README.md) 源码作为历史开发与测试样例保留，本地商店库已移除，不计入当前四个演示数据库；按需重建或扩充的说明见其 README。

## 使用

1. 创建所需数据库。
2. 在管理控制台添加该 SQLite 文件；可参考 `rewrite.yml`、SQL 示例、Action 和视图配置。
3. 需要一次性导入整套配置时，使用平台的通用 `import-config` 工具。新业务账号的初始密码通过 `DATASCOPE_EXAMPLE_PASSWORD` 环境变量提供，至少 12 字符。

在仓库根目录执行：

```sh
make check-example EXAMPLE=securities
make import-example EXAMPLE=securities
# EXAMPLE 也可以为 regional-sales、academic 或 channel-operations；shop 仅用于历史开发样例。
```

发行包中直接运行 `import-config -file /path/to/examples/securities/bundle.yml` 即可，无需编译示例代码。
路径相对于 `bundle.yml`，资源文件位于包内，数据库路径可指向包外的 `data/`。
重复导入保留已有同 ID、同目标的数据源，包括认证方式、账号密码、停用状态和自定义配置；不同目标拒绝覆盖。
已有库的后续修改使用管理控制台或管理 MCP。导入器不会将示例配置自动同步到已有库。

服务与生产镜像默认空数据源启动，不加载、生成或安装这些示例。
Go 测试辅助代码和验收数据独立放在 `tests/`；它们不参与主服务构建。

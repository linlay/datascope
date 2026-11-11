# 数据示例

本目录只保存 SQL、声明式配置、业务说明及可选的 Python 造数脚本，不包含 Go 包、Go 安装器或服务启动逻辑。

| 示例 | 数据来源 | 创建数据库 |
| --- | --- | --- |
| [证券](securities/README.md) | `schema.sqlite.sql` + `seed.sql`，小规模虚构数据 | `make init` |
| [商店](shop/README.md) | `schema.sqlite.sql`，含种子数据 | `make init-shop` |
| [地区销售](regional-sales/README.md) | DDL + Python 标准库造数脚本 | `make init-regional-sales` |

实际 SQLite 文件生成到 `data/`，拒绝覆盖已有文件；也可以用 SQLite 客户端直接执行 SQL。
证券示例提供小规模可重建数据，完整大规模数据只用于自动测试，不把数百 MB 数据库放入源码仓库。

## 使用

1. 创建所需数据库。
2. 在管理控制台添加该 SQLite 文件；可参考 `rewrite.yml`、SQL 示例、Action 和视图配置。
3. 需要一次性导入整套配置时，使用平台的通用 `import-config` 工具。新业务账号的初始密码通过 `DATASCOPE_EXAMPLE_PASSWORD` 环境变量提供，至少 12 字符。

在仓库根目录执行：

```sh
make check-example EXAMPLE=shop
make import-example EXAMPLE=shop
# EXAMPLE 也可以为 securities 或 regional-sales。
```

发行包中直接运行 `import-config -file /path/to/examples/shop/bundle.yml` 即可，无需编译示例代码。
路径相对于 `bundle.yml`，资源文件位于包内，数据库路径可指向包外的 `data/`。
重复导入保留已有同 ID、同目标的数据源，包括认证方式、账号密码、停用状态和自定义配置；不同目标拒绝覆盖。
已有库的后续修改使用管理控制台或管理 MCP。导入器不会将示例配置自动同步到已有库。

服务与生产镜像默认空数据源启动，不加载、生成或安装这些示例。
Go 测试辅助代码和验收数据独立放在 `tests/`；它们不参与主服务构建。

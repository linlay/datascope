# 自动测试

`testutil/` 保存框架测试的隔离夹具；其中证券数据生成、账号构造和权限参考算法只供 Go 测试使用，不由主服务或通用导入器引用。
`fixtures/` 保存独立预期和回归验收数据，`browser/` 保存浏览器验收脚本，`examples/` 验证 SQL/Python 数据示例及声明式导入。
`architecture/` 同时检查：生产源码不依赖测试或示例包，examples 中不允许出现 Go 源码。
用户使用 examples 不需要运行这些测试辅助代码。

渠道运营示例通过 `python3 -m unittest discover -s tests/examples -p 'channel_operations_test.py'` 和 `go test ./tests/examples/channel-operations ./internal/configbundle ./tests/architecture -count=1` 验证。覆盖确定性、拒绝覆盖、失败清理、归因与指标边界、跨账号及内容权限、成本隔离、授权到期、十个 SQL 示例和六个 Action 的正常操作、拒绝、重复迁移、预览回滚及确认提交。所有写入使用隔离数据库。

## 示例数据维护回归

```sh
python3 -m unittest discover -s tests/examples -p '*_test.py'
go test ./tests/examples/shop ./tests/testutil/securities/... ./internal/service ./internal/rewrite ./internal/action ./internal/configbundle
```

Python 检查覆盖备份、拒绝覆盖、追加保留原记录、重复执行、确定性、半年分布、边界数据、快照及交易窗口、回归账户历史保留、原值与索引不变、无效保留参数拒绝及账务校验失败回滚。shop 的 Go 测试通过生产权限改写器验证身份 1–4 及未知身份的客户、订单、销售额、OR 绕过、商品公开目录及成本字段隐藏。

针对维护后的本地证券库，指定缩减前备份与缩减后库；测试只读查询。覆盖四个授权生效日期、身份 1–179 的客户和账户范围、2026-10-20 持仓及资金日结，以及 2026-09-15 至 2026-10-20 委托、成交和资金流水聚合，共 5,012 次结果对比：

```sh
DATASCOPE_SECURITIES_BEFORE_DB=/absolute/path/to/backup.sqlite3 \
DATASCOPE_SECURITIES_AFTER_DB=/absolute/path/to/compacted.sqlite3 \
go test ./internal/service -run TestLocalCompactedSnapshotPermissions -count=1 -v

# 复制本地库到临时文件，执行正常和拒绝案例的 Action 预览；原库不写入。
DATASCOPE_DEMO_ACTIONS_TEST_DB=/absolute/path/to/compacted.sqlite3 \
go test ./tests/testutil/securities/actions -count=1 -v

DATASCOPE_SHOP_TEST_DB=/absolute/path/to/shop.sqlite3 \
go test ./tests/examples/shop -count=1 -v
```

Action 正式执行、依赖准备步骤、本人确认、重复请求及回滚由隔离夹具验证；本地完整库验收仅运行不需要提交准备步骤的预览案例，不向原库写业务数据。备份和业务库均保留在 Git 忽略的 `data/` 中。

高校教务示例使用 `go test ./tests/examples/academic ./internal/configbundle ./tests/architecture -count=1`。生成隔离的 2026-09-10 与 2026-11-13 快照，验证五类身份、辅导员交接、成绩发布与联系方式隔离、SQL 示例、Action 预览回滚、确认提交以及越权和超期拒绝。Python 示例测试同时检查确定性、拒绝覆盖、失败清理、校历、重修、满班、空班、缺考和未来成绩。教务业务只存在于 `examples/academic/` 及测试代码，生产服务不会自动生成或导入。

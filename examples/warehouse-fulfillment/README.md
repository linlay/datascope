# 仓储履约演示数据库

与[销售交易演示库](../sales-transactions/README.md)配套的小型上游 SQLite，约 68 KiB。两个库通过共同的订单、订单明细、商品和退货编号关联，为区域销售库提供可核验的业务数据。

- 2 个仓库、20 张订单引用、40 条订单明细引用。
- 初始 15 张发货单、30 条发货明细，含一张部分发货订单。
- 1 张已收货退货单及其明细；对应退款位于交易来源。
- 12 行 `2026-11-15` 库存快照，供后续修订演示；没有库存流水账，不用于还原历史周转。
- `order_changes`、`stock_changes` 保存对象的最新变更序号，支持增量抽取。

统一使用 `make init-supply-chain-demo` 创建两个库，不单独生成互不相关的仓储数据。完整创建、接入、抽取顺序、增量推进和核对步骤见销售交易库 README。

`make import-example EXAMPLE=warehouse-fulfillment` 使用通用配置导入器登记此库，创建只读业务账号 **WMS001**（业务身份 1）。初始密码通过 `DATASCOPE_EXAMPLE_PASSWORD` 提供，不写入仓库。来源不提供 Action，导入配置不自动开启抽取计划。

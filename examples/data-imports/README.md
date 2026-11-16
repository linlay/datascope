# 外部数据库导入体验

全部使用虚构数据，但来源运行真实 Oracle、PostgreSQL 和 MySQL，支持已登记的分地区销售、高校、渠道运营和证券四个演示库，每库各三条任务。不会修改原有客户、订单、库存表，也不会改变原有业务账号、认证或权限。

| 来源 | 目标接入表 | metric_value 含义 | 自动计划 |
|---|---|---|---|
| Oracle | imported_erp_customers | 客户信用额度，人民币分 | 每 2 分钟 |
| PostgreSQL | imported_warehouse_inventory | 可用库存件数 | 每 3 分钟 |
| MySQL | imported_online_orders | 线上订单金额，人民币分 | 每 5 分钟 |

其他三个演示库复用同一组数据库容器，分别读取自己的来源事件表，目标接入表和进度相互独立：

| 演示库 | Oracle 案例 / 接入表 | PostgreSQL 案例 / 接入表 | MySQL 案例 / 接入表 |
|---|---|---|---|
| 高校 | 学籍档案 / imported_student_registry | 考勤汇总 / imported_attendance_summary | 在线选课 / imported_course_signups |
| 渠道运营 | 合作账号 / imported_partner_accounts | 内容指标 / imported_content_metrics | 线索转化 / imported_lead_conversions |
| 证券 | 客户档案 / imported_customer_profiles | 行情快照 / imported_market_quotes | 开户申请 / imported_account_applications |

四库均按 Oracle 每 2 分钟、PostgreSQL 每 3 分钟、MySQL 每 5 分钟运行；每库都有自己的三个外部连接、三个接入表、三条任务和运行记录。

每个场景的三个来源各准备 720 个变更事件，包含 100 个业务对象的新增、更新及软删除。每次最多处理 40 个已经到达发生时间的事件；前 120 个事件在生成时已发生，其余每 5 秒到达一个。数据是预生成的有限事件流，不是持续随机造数；水位到 720 后，任务正常空跑。修改 `batchSize` 可更快消费。再次生成文件不会给运行中的来源自动补数。

## 启动

先有已登记的分地区销售数据库和现有平台管理员，在仓库根目录执行：

```sh
make prepare-import-demo
make start-import-demo
make seed-import-demo
make import-source-demo
```

首次为另外三个**尚未装载案例**的库安装时，分别执行（来源容器只需启动一次）：

```sh
for scenario in academic channel-operations securities; do
  make prepare-import-demo IMPORT_SCENARIO="$scenario"
  make seed-import-demo IMPORT_SCENARIO="$scenario"
  make import-source-demo IMPORT_SCENARIO="$scenario"
done
```

`IMPORT_SCENARIO` 默认为 `regional-sales`。其他场景的清单和生成 SQL 放在 `data/import-demo/<场景>/`，共用顶层 `compose.env`；不要给同一组运行中的容器重新生成不同密码。

准备步骤为目标做 SQLite 一致性备份后，只添加三个接入表；本地文件、生成 SQL 和随机密码位于 Git 忽略的 `data/import-demo/`。Compose 只监听本机 13316、15432、11521 端口，使用独立项目和卷。首次启动需下载 PostgreSQL、Oracle 镜像。三个来源账号只授予 SELECT，导入器另外建立只读事务。

`seed` 只用于首次装载，发现已有表会失败并停止，不删除现有数据。已有任务再次导入默认保留连接、脚本、开关、计划和增量进度；不要通过重复装载重置正在使用的来源。

导入命令先刷新目标结构（新增表默认禁止业务查询），登记连接和任务，再对每个任务完成一次回滚试运行及一次正式运行。自动计划按任务保存时间计算下次执行，使用现有平台管理员作为执行者；后台 admin-api 必须运行。

## 在界面体验

1. 选择已安装案例的演示库 →「数据来源」。三个连接有真实驱动类型，可在「外部连接」查看来源表结构；三个导入任务都有已保存脚本、参数和自动计划。
2. 打开任务，点击「回滚试运行」，查看日志和影响行数；目标数据和正式进度均不变。
3. 点击「立即运行」，再打开「数据运行记录」，查看 `read`、新增/更新计数、水位、实际参数和脚本版本。
4. 打开「表数据」选择接入表，对比 `source_seq`、`source_updated_at` 与 `imported_at`。稍后可看到由定时触发的记录及新的水位。接入表供平台管理员核验，未自动向业务查询账号开放。
5. 正式运行时临时覆盖参数为 `{"batchSize":40,"simulateFailure":true}`，可体验写入后主动失败和整次回滚。检查失败记录后，使用默认参数重跑，水位从上次成功处继续。
6. 软删除保留记录，标为 `is_deleted=1`；有效记录需过滤 `is_deleted=0`。消费的是单调事件编号，更新旧对象也能被发现；`source_seq` 防止同一事件重放覆盖较新数据。

时区统一为 Asia/Shanghai。来源事件时间与导入时间保存为 UTC；金额使用整数分，脚本验证整数在 JavaScript 安全范围内。Oracle 大写字段与其他驱动的小写字段均有处理。

## 暂停和恢复

在每个任务中关闭自动运行，即可暂停。`make stop-import-demo` 停止三个来源容器并保留数据卷；再次 `make start-import-demo` 恢复来源，**不要重新 seed**。暂停来源前宜关闭任务自动开关，避免留下预期的连接失败记录。

## 通用配置导入器

`cmd/import-sources` 接受单个 JSON 配置（`version:1`、`database_id`、`connections`、`tasks`）。连接使用现有连接字段，增加 `password_env` 从环境读取密码；任务结构和数据来源管理接口一致，脚本直接保存在 `script` 字段。没有建业务表或业务造数逻辑。

```sh
# shell 中先加载本地随机凭据；不要把密码放在命令行参数或仓库中
set -a; . data/import-demo/compose.env; set +a
go run ./cmd/import-sources -file data/import-demo/sources.json -check
```

`-check` 检查 JSON、任务 JS/计划和来源连通性，不保存连接和任务，不执行导入；可能初始化空的数据来源状态库。`-discover` 刷新目标表结构，新增表保留默认禁止策略。`-replace` 显式更新同名连接和任务，保留正式进度；默认保留已有配置。`-run` 对清单中的任务各执行一次回滚试运行和正式运行。多条配置按顺序保存，不是跨配置事务；失败后可重跑，已经登记的条目默认保留。新增任务可在清单中设置 `enabled:true` 启用自动计划。

底层接口及限制见 [数据来源文档](../../docs/data-sources.md)。镜像设置参考 [PostgreSQL 官方镜像](https://hub.docker.com/_/postgres)和 [Oracle Free 镜像维护者文档](https://github.com/gvenzl/oci-oracle-free)。

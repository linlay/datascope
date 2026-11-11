# SQLite DDL 管理

所有参数放在 `args`，并携带 `resourceType:"ddl"`、`database_id`。只适用于已打开 `ddl_enabled` 的本地 SQLite 业务数据源。管理与审计数据库及其别名不能操作。普通数据库 MCP 不提供这些能力。

系统管理员通过 `datascope_manage` 的 `database.update` 开关本功能：

```json
{"action":"update","args":{"resourceType":"database","database_id":"default","database":{"id":"default","revision":当前版本,"ddl_enabled":true}}}
```

关闭时传 false。先 database.get 获取真实 ID、最新 revision。查看需要 config.read，预览与执行另外需要 ddl.write；系统管理员默认拥有全部权限。数据库开关与 Action 写入开关独立。

| 工具 | action | 参数 | 返回 |
| --- | --- | --- | --- |
| datascope_query | list | 可选 object_type、query、limit、offset | items、total |
| datascope_query | get | object_type、name | sql、definition、dependencies |
| datascope_inspect | preview | sql 或 changes，二选一 | plan_id、diff、sql、warnings、requires_rebuild |
| datascope_manage | apply | plan_id、request_id | execution_id、status、backup_id、discovery_status |
| datascope_query | execution | execution_id | 执行详情、SQL、差异、错误与备份编号 |
| datascope_query | history | 可选 limit、offset | items、total |

object_type 为 table、index、view、trigger；结构化 changes 另支持 column、constraint。list 默认每页 50、最多 200；history 默认 20、最多 100。

预览示例：

```json
{"action":"preview","args":{"resourceType":"ddl","database_id":"default","changes":[{"operation":"update","object_type":"column","parent_name":"customers","name":"email","definition":{"sql":"email TEXT NOT NULL DEFAULT ''"}}]}}
```

changes 每项字段：operation=create/update/rename/drop；object_type；name；字段、约束及索引的 parent_name；改名的 new_name；创建或修改的 definition。

- 表 definition：完整 CREATE TABLE 的 sql，或 columns、constraints（SQL 字符串数组）、strict、without_rowid。columns 每项包含 name、type、not_null、default（SQL 表达式字符串）、primary_key（0/1），也可用 sql 表达完整字段定义。
- 字段 definition：sql 为单个完整字段定义，或 columns 中的一项；update 替换整个字段定义，保留仍需要的约束。
- 约束 definition：sql 为单个表约束；修改/删除时 name 使用约束名，未命名约束用 constraint_序号（从 1 开始，对应 definition.constraints）。
- 索引 definition：sql，或 columns（每项 name）、unique、where。表达式索引用完整 CREATE INDEX SQL。
- 视图和触发器 definition：完整 CREATE VIEW / CREATE TRIGGER 的 sql。

表定义更新按同名字段迁移数据，未映射的旧字段将删除；改名使用 rename 操作。目标为生成列时由 SQLite 重新计算。复杂修改会重建表，检查关联索引、触发器、视图与外键，保留自增序列。

先向用户展示 diff、warnings 和实际 SQL，在已有授权范围内执行计划。plan_id 与当前用户、数据库、配置版本和结构绑定，有效期 30 分钟。执行时重新校验，并在单个事务中执行整组 SQL。不可直接通过 SQL 入口运行 DML、ATTACH、PRAGMA、事务控制或临时对象；触发器自身的业务 DML 定义允许。

```json
{"action":"apply","args":{"resourceType":"ddl","database_id":"default","plan_id":"预览返回值","request_id":"本次唯一请求ID"}}
```

同一计划只执行一次。请求超时或结果不确定时，先 history / execution 查状态，不能换 request_id 盲目重跑。failed 表示事务失败；interrupted 表示需要核对源结构；succeeded 表示结构已提交，即使 discovery_status=failed 也不应重放 DDL。

成功后由服务端侦查结构，使用 discovery.get / refresh 查看，按现有 discovery.apply 流程审核。新增表默认关闭、字段白名单不自动扩大；权限 SQL 和 Action 不自动重写。不同服务进程的侦查候选独立，必要时在当前管理入口 refresh。

执行前自动保存一致性业务数据库备份与管理 YAML/密钥快照，位于服务端 `data/databases/<ID>/ddl/backups/<backup_id>/`（实际根目录跟随管理存储）。不将备份文件通过 MCP 直接返回。恢复备份是独立运维操作，需协调所有连接与后续业务写入。

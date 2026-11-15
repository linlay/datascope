# 数据来源与脚本导入

在当前目标数据库下打开「数据来源」。本模块独立于业务 Action，配置页采用与动作配置一致的布局：左侧「导入列表」、右侧「导入详情」；「外部连接」通过列表标题旁的按钮打开弹窗，支持连接管理与结构查看，执行记录位于左侧独立菜单「数据运行记录」（在「查询工作台」前）；外部连接不需要登记为对外服务的业务数据库，也不会自动发布 MCP。

访问和操作均需要当前库的 `config.read` 与 `source.write`。`source.write` 是显式的数据库搬运权限，允许读取任务选择的来源连接，并向当前目标库写入，不自动应用业务查询的行列策略和脱敏；不要向普通查询账号授予此能力。自动运行使用最近保存任务的平台用户，启动前、执行期间及提交前重新检查授权。平台业务身份不能调用本模块。

## 外部连接

支持现有 SQLite、MySQL、PostgreSQL、Oracle、达梦、OceanBase（MySQL 模式）驱动，SQL 沿用项目已有的受限可移植方言，并非所有数据库原生语法。网络库仍需实际环境验收。

连接别名以字母开头，最多 64 个字母、数字、下划线或短横线。创建后保持别名不变，任务通过别名引用。密码以当前目标数据库的 `credentials.key` 加密保存，接口不返回明文或密文；编辑时留空保留密码。来源只提供只读事务，SQLite 文件必须在允许目录内，内部管理、审计及数据来源状态文件和其文件别名不能作为来源。SQLite 来源只能连接已有文件。

连接可以被多个任务使用。删除前必须解除所有任务引用。连接配置在启动时取得快照，之后的编辑不改变已启动任务。

## 导入任务

目标表必须预先存在。SQLite 目标由 `source.write` 授权使用独立写连接，不依赖 Action 开关；网络目标复用「动作配置」中已启用的独立写入账号，账号必须指向当前库。当前没有自动建表、UPSERT、DDL、CDC 或跨库分布式事务。

任务先保存，再回滚试运行或正式执行。两者均运行保存的脚本版本，可临时覆盖默认 JSON 参数。试运行执行整个脚本并回滚目标事务，不推进正式进度；它不是自动抽样。正式运行整次使用一个目标事务，正常返回才提交，未捕获异常、SQL 错误、超时、停止均回滚。SQL 错误即使被 JS 捕获仍使本次运行失败。提交失败或提交后进度保存失败标记「结果未知」，须核对目标数据后再运行。

```js
function main(params, context) {
  const source = sources.get("external");
  const target = db.target();
  let lastId = context.checkpoint.lastId || 0;
  while (true) {
    const rows = source.query(
      "SELECT id, name FROM records WHERE id > ? ORDER BY id LIMIT 500",
      [lastId]
    );
    if (rows.length === 0) break;
    for (const row of rows) {
      const existing = target.query("SELECT id FROM imported WHERE id = ?", [row.id]);
      if (existing.length === 0) {
        target.update("INSERT INTO imported(id, name) VALUES(?, ?)", [row.id, row.name.trim()]);
      } else {
        target.update("UPDATE imported SET name = ? WHERE id = ?", [row.name.trim(), row.id]);
      }
    }
    lastId = rows[rows.length - 1].id;
  }
  context.saveCheckpoint({lastId});
  context.log("Import finished");
}
```

示例按递增主键导入新增记录，不会自动发现旧记录更新或删除。业务需要处理更新时，由脚本明确实现更新时间、水位、重叠窗口及去重规则。

| 接口 | 行为 |
| --- | --- |
| `main(params, context)` | 同步入口；参数来自本次运行的 JSON 对象 |
| `sources.get(alias)` | 获取本任务选择的来源连接，不允许任意 DSN 或其他未选连接 |
| `source.query(sql, values?)` | 只读 SELECT，返回行数组，使用 `?` 和标量数组绑定值 |
| `db.target()` | 当前目标库连接；不接受其他数据库 ID |
| `target.query(sql, values?)` | 在本次目标事务内读取，返回行数组 |
| `target.update(sql, values?)` | 单条受限 INSERT/UPDATE/DELETE，返回 `{rowsAffected,lastInsertId}`；UPDATE/DELETE 要求 WHERE |
| `context.databaseId / runId / now / trial` | 目标库 ID、运行编号、固定 UTC 时间、是否试运行 |
| `context.checkpoint` | 本任务上次正式成功后保存的 JSON 进度，初始为空对象 |
| `context.saveCheckpoint(object)` | 设置待保存进度，整个任务提交后才持久化，最多 64 KiB |
| `context.log(message, ...)` | 写入运行日志；不要记录敏感业务数据 |

SQL 值仅支持 null、字符串、布尔和安全范围内的有限数字。BIGINT、DECIMAL、NUMERIC 沿用精确字符串表示；驱动返回有损浮点时拒绝。JS 可以按业务逻辑选择 SQL，但所有 SQL 在执行时经过现有语句和 Schema 校验；没有网络、文件、进程、npm、CommonJS 公共库或事件循环能力，不支持 async/Promise。

执行默认最长 300 秒，可设为 1–3600 秒。最多执行 100000 条 SQL、读取累计 100 万行；每次查询最多 1000 行、结果 8 MiB，超过限制报错，不能把截断结果当完整迁移。脚本应使用排序及游标分页。日志最多 200 条、每条 2048 字节，超过条数拒绝执行。

来源连接在首次访问时打开独立只读事务；多个来源之间没有统一快照。目标提交和管理进度保存不是分布式事务：若两者之间中断，可能重放上次数据，应编写幂等脚本。服务非正常退出后保留记录并标记中断，提交结果可能未知；不会自动重试该次运行。

## 运行计划与记录

支持仅手动、指定时间、固定分钟间隔、每天指定时间。新任务默认关闭自动运行；选择时区并检查预计运行时间，保存后开启。指定时间在存储时保留明确 UTC 时刻；每天运行按所选时区计算。指定时间执行一次后自动关闭。固定间隔以下次启动时间为基准，不是固定日历刻度。

调度只在 admin-api 进程运行，每 5 秒检查。错过多个时间点合并执行一次，失败不自动重试。同一任务通过跨进程文件锁禁止重叠；不同任务可以并行，但目标写入仍受数据库锁控制。关闭页面不停止后台执行，点击运行详情的「停止运行」才请求取消。

「数据运行记录」支持按任务筛选；任务编辑页可以直接打开当前任务的记录。规范路由为 `/#/data-run-history?database=<ID>`，筛选任务使用 `task=<任务 ID>`，查看某次运行使用 `/#/data-run-history/<运行 ID>?database=<ID>`。旧的 `sourceTab=runs` 和 `taskTab=history` 链接会跳转到新入口。

运行记录包含任务版本、原始脚本快照、实际参数、触发方式、状态、日志、读取和影响行数。影响行数是执行计数，试运行、失败或停止时不能视为已提交行数。列表展示最近 100 条记录，记录本身持久保存在当前目标库的 `data/databases/<ID>/data-sources/state.db`，密码使用同级目标库的 `credentials.key`。备份须同时保存状态库与密钥。

## 管理接口

通过已登录的平台会话调用 `POST /api/management/call`，格式为 `{action,arguments:{database_id,...}}`。新增接口尚未发布为管理 MCP 操作。

| action | 附加参数 |
| --- | --- |
| `list_data_sources` | 无，返回 connections/tasks/runs 概览 |
| `save_source_connection` | `source_connection`、可选 `password`；编辑时包含当前 revision |
| `inspect_source_connection` | 已保存连接 `id`，或草稿 `source_connection` + `password`；可选 `table` 读取结构 |
| `delete_source_connection` | `id` |
| `save_import_task` | `import_task`；编辑时包含当前 revision |
| `delete_import_task` | `id` |
| `run_import_task` / `trial_import_task` | 任务 `id`、可选 `parameters`；返回运行编号 |
| `get_import_run` | 运行 `id`，返回日志及脚本快照 |
| `stop_import_run` | 运行 `id` |

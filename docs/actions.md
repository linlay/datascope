# 业务操作 Action

Action 是数据库配置中的 `actions` 数组，和业务查询工具一起保留配置版本。每项仅有 `name`、`title`、`description`、`confirm`、`parameters`、`script` 六个字段。

## 配置和权限

在「MCP 配置 → 业务操作 Action」启用独立写入连接，新增操作后保存。SQLite 使用原业务文件的独立 `mode=rw` 连接，不创建新文件，并关闭把不存在的双引号字段当作字符串的旧式回退；普通查询仍用 `mode=ro`、`query_only`。网络库配置独立写入账号，目标数据库、schema、TLS 必须与查询连接一致。密码可直接输入并加密保存，也可使用 `DATASCOPE_DB_` 环境变量引用；密码不进入配置、版本历史和审计。

编写脚本、启停写入、配置或轮换写入账号需要平台权限 `action.write` 和 `config.read`。`mcp.write` 不允许修改这些字段。控制台试运行另外需要 `impersonate`，可填写业务身份编号。新增能力不会自动授予旧业务账号。

在「开放工具」启用 `describe_action`、`invoke_action`，并为当前数据库的业务账号授予 `action`。业务账号可以只有 `action`，与 `query` 并列；该账号不能登录管理后台。`my.*` 始终应用数据库已有查询策略，不需要额外授予业务账号通用查询工具。

## 定义示例

下面使用示意表，实际应替换为数据库中的真实名称。金额采用整数分，参数整数限定在 JavaScript 安全整数范围。

```yaml
actions:
  - name: rename_item
    title: 修改本人记录名称
    description: 修改可见且归本人管理的记录名称
    confirm: true
    parameters:
      id: {type: integer, description: 记录编号, min: 1}
      name: {type: string, description: 新名称, max_length: 100}
    script: |
      function run(args, ctx) {
        const row = my.one`SELECT id, owner_id FROM items WHERE id = ${args.id}`;
        if (!row) deny('记录不存在或无权查看');
        if (row.owner_id !== ctx.userId) deny('只能修改本人记录');
        const n = db.exec`UPDATE items SET name = ${args.name}
                          WHERE id = ${args.id} AND owner_id = ${ctx.userId}`;
        if (n !== 1) deny('记录已变化，请重试');
        const updated = my.one`SELECT id, name FROM items WHERE id = ${args.id}`;
        if (!updated || updated.name !== args.name) deny('后置检查失败');
        return updated;
      }
```

参数支持 `string`、`integer`、`number`、`boolean`，默认必填；可加 `optional: true`、`enum`、`min`、`max`、`max_length`、`pattern`。拒绝未知参数和 null；校验错误附完整 JSON Schema。`max_length` 按 Unicode 字符计算。`pattern` 使用 Go 正则语法。

| 脚本接口 | 行为 |
| --- | --- |
| `my.one` / `my.all` | SELECT 自动应用表、字段、行策略、deny_unless 和各表行数上限 |
| `db.one` / `db.all` | SELECT 读取可信业务数据，绕过查询策略 |
| `db.exec` | 执行单条 INSERT / UPDATE / DELETE，返回影响行数 |
| `deny(message)` | 中止事务，拒绝原因返回给调用方 |
| `ctx.userId` / `ctx.now` | 可信业务身份、本次操作的固定 UTC RFC3339 时间 |
| `return value` | 同步返回 JSON 值，最多 1 MiB |

所有 SQL 必须写成标签模板，如 ``db.one`SELECT ... WHERE id=${args.id}` ``。编译时固定每个模板的 SQL，插值只能作为参数值绑定；字符串拼接、动态模板、引号或注释内的插值均不能执行。表和列名直接写在模板中。

`one` 无记录时返回 null，`all` 返回数组。BIGINT、DECIMAL、NUMERIC 作为字符串返回；其他超出安全整数范围的整数也返回字符串。若驱动已把精确小数转换成浮点，拒绝返回，需在 SQL 中将该字段转为文本。`db.*` 读出的内容不自动脱敏，脚本作者应只返回业务需要的结果。

每次操作共享一个事务，前置/后置 SELECT 都能看到本事务的修改。正常返回才提交；deny、SQL 异常、JS 异常、超时均回滚，即使脚本捕获了 deny/SQL 异常也不能提交。限制为总时限 5 秒（包括连接建立）、50 条脚本 SQL（包括权限 guard）、每次 SELECT 最多 1000 行。脚本没有网络、文件、进程、模块加载或计时器接口。

UPDATE / DELETE 必须有 WHERE，并建议再次包含所有关键权限和状态条件、检查影响行数。普通 INSERT、UPDATE、DELETE 经 AST 编译为各数据库的参数和引用形式；网络库暂不支持 DML 的 WITH、UPDATE FROM、RETURNING、UPSERT、冲突处理、DELETE ORDER/LIMIT。SQLite 也禁止事务控制、DDL、PRAGMA、ATTACH、REPLACE、RETURNING 和冲突处理。写入账号本身应只获得必需表的权限。

## MCP 调用和人工确认

```json
{"name":"rename_item","arguments":{"id":1,"name":"新名称"},"request_id":"unique-operation-uuid"}
```

`describe_action({})` 列出可用操作，传 `name` 返回参数 Schema。`invoke_action` 返回 `done`、`denied` 或 `confirm_required`。

`confirm: true` 会先完整执行并回滚，返回 `preview.result`、`preview.writes`（SQL、绑定参数、影响行数）、`confirmation_token` 和 `confirmation_url`。确认预览保留 10 分钟。

业务连接器可显示：

````markdown
```view key=datascope/action
{"title":"确认修改记录","preview":{"result":{"id":1,"name":"新名称"},"writes":[]},"confirmation_url":"从 invoke_action 响应原样复制"}
```
````

必须原样复制服务端返回的预览和链接。人在视图点击「打开确认页」，使用发起操作的同一个业务账号登录并确认。独立页面要求密码、浏览器 CSRF Cookie 和 nonce；两个 MCP 都没有确认接口，模型仅持有令牌不能提交。确认页会展示真实的服务端预览。确认后，用原始 name、arguments、request_id 再次调用 `invoke_action` 获取结果。

正式执行前重新检查账号权限、账号版本、认证配置、数据库配置版本和 MCP 启用状态。再次执行脚本，并比较返回值及完整写入计划（SQL、绑定参数、影响行数）；变化则回滚，要求新的 request_id 重新预览。固定 ctx.now 沿用首次预览时间。

## 幂等、审计和故障恢复

request_id 按数据库和发起账号隔离。相同 ID、操作和参数只执行一次，已完成调用直接返回历史结果；同一 ID 换操作、参数或身份会拒绝。预览和完成记录存于管理 SQLite，重启不丢失；并发调用和重复点击由数据库唯一键及状态转换保护。

管理库与业务库是独立数据库，无法跨库原子提交。进程可能在业务提交后、保存返回值前崩溃；这种请求持久保持 `running`，返回「执行中或结果未知」，绝不自动重新写入。管理员应结合 request_id 审计及业务数据人工核对，不能通过换 ID 盲目重试。备份管理库时须保留 action_requests，避免丢失幂等记录。

每条 SQL 在执行前同步保存 SQL 和绑定参数，完成后记录影响/返回行数及错误；整体事件保存参数、结果、预览标志和拒绝原因。提交前同步保存结果审计，失败即回滚。提交结果或提交后审计结果不确定时保留未知状态。密码与确认令牌不进入 SQL/操作审计。

控制台「试运行」始终回滚，包括 confirm:false 的操作，显示 SQL 审计和执行预览。管理 MCP 使用 `datascope_manage` 的 `{action:"save",args:{resourceType:"action",database_id,database,write_password?}}` 保存 action、写连接和启用状态；使用 `datascope_inspect` 的 `{action:"try",args:{resourceType:"action",database_id,name,arguments,request_id,business_user_id}}` 试运行，始终回滚。读取当前配置与 revision 后再保存。

SQLite 已有事务、权限、幂等、确认和并发端到端回归；网络库使用共享连接和 AST 编译，并有各驱动 SQL 编译/绑定测试。接入真实 MySQL、PostgreSQL、Oracle、达梦写账号后，仍需在目标库验收其账号权限、字段精度、锁和超时行为；测试未配置实际写账号时不代表已做真实库写入验收。

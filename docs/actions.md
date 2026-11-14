# 业务操作 Action

定时从外部数据库读取并转换、导入当前库，使用独立的「数据来源」模块，见[数据来源与脚本导入](data-sources.md)。其 `sources.get`、`db.target` 和进度接口不属于 Action 脚本契约，Action 的单库事务及执行限制保持不变。

Action 通过 API 保持数据库配置中的 `actions` 数组契约，实际保存到 `management/actions/<action>.yml`，并使用数据库当前 revision 控制并发保存。每项包含 `name`、`title`、`description`、`confirm`、`parameters`、`script`，以及可选的 `view_id`（YAML 为 `viewId`）和 `published`。

动作、知识与视图列表右侧的开关直接保存发布状态，不提交详情中尚未保存的编辑。新增和复制的动作默认是草稿；旧动作未配置 `published` 时保持已发布。动作取消发布后，业务 MCP 不再列出或描述它，也拒绝直接调用和未完成的确认；平台编辑者仍可查看与回滚试运行。发布与人工确认分别控制“是否可供业务调用”和“调用是否需要本人审核”。人工确认开关旁的帮助入口提供侧边说明。视图被默认查询或动作引用时，需先取消关联才能取消发布。

## 配置和权限

在「动作配置」启用独立写入连接，新增操作后保存。SQLite 使用原业务文件的独立 `mode=rw` 连接，不创建新文件，并关闭把不存在的双引号字段当作字符串的旧式回退；普通查询仍用 `mode=ro`、`query_only`。网络库配置独立写入账号，目标数据库、schema、TLS 必须与查询连接一致。密码可直接输入并加密保存，也可使用 `DATASCOPE_DB_` 环境变量引用；连接密码密文只进入 database.yml 的专用字段，密码明文不进入配置、接口返回或审计。

编写脚本、启停写入、配置或轮换写入账号需要平台权限 `action.write` 和 `config.read`。`mcp.write` 不允许修改这些字段。控制台试运行另外需要 `impersonate`，可填写业务身份编号。新增能力不会自动授予旧业务账号。

在「开放工具」启用 `list_actions`、`describe_action`、`invoke_action`，并为当前数据库的业务账号授予 `action`。业务账号可以只有 `action`，与 `query` 并列；该账号不能登录管理后台。`my.*` 始终应用数据库已有查询策略，不需要额外授予业务账号通用查询工具。

## qiuer 风格脚本

新 Action 参照 qiuer-go 正式业务 JS 契约：入口为 `main(params, context)`，公共库使用 CommonJS，相对 `require` 只解析当前数据库的公共库。DataScope 仍使用自身的 Goja 执行器与事务管理，不需要部署 qiuer 服务；开放的是受限的 Action API，并非 qiuer 全部宿主能力。

```yaml
actions:
  - name: rename_item
    title: 修改本人记录名称
    description: 修改归本人管理的记录名称
    confirm: true
    parameters:
      id: {type: integer, description: 记录编号, min: 1}
      name: {type: string, description: 新名称, maxLength: 100}
    script: |
      function main(params, context) {
        const conn = db.session();
        const row = conn.queryOne("SELECT id, owner_id FROM items WHERE id=:id", {id: params.id});
        if (!row || row.owner_id !== context.userId) throw new HttpError(403, "无权修改此记录");
        const result = conn.update(
          "UPDATE items SET name=:name WHERE id=:id AND owner_id=:userId",
          {name: params.name, id: params.id, userId: context.userId}
        );
        if (result.rowsAffected !== 1) throw new HttpError(409, "记录已变化，请重试");
        const updated = conn.queryOne("SELECT id, name FROM items WHERE id=:id", {id: params.id});
        if (!updated || updated.name !== params.name) throw new Error("后置检查失败");
        return updated;
      }
```

参数支持 `string`、`integer`、`number`、`boolean`，默认必填；可加 `optional: true`、`enum`、`min`、`max`、`maxLength`、`pattern`。拒绝未知参数和 null；校验错误附完整 JSON Schema。`maxLength` 按 Unicode 字符计算，`pattern` 使用 Go 正则语法。

| 脚本接口 | 行为 |
| --- | --- |
| `db.session()` | 当前 Action 所属数据库的连接；本次调用及公共库共享同一事务 |
| `db.session(databaseId)` | 显式指定当前数据库也可使用；预留额外数据库入口，目前拒绝其他 ID |
| `conn.query(sql, params?)` | SELECT，返回行对象数组 |
| `conn.queryOne(sql, params?)` | SELECT，返回首行或 null |
| `conn.update(sql, params?)` | 单条 INSERT/UPDATE/DELETE，返回 `{rowsAffected,lastInsertId}`；部分驱动不提供 lastInsertId |
| `context.userId / databaseId / now` | 可信业务身份、当前数据库 ID、本次操作固定 UTC 时间 |
| `throw new HttpError(status,message,details?)` | 拒绝操作；未捕获异常使整次事务回滚 |
| `list/map/string/coalesce/typeOf/contains/nil` | 与 qiuer 同名的普通值处理助手；contains 默认读取当前入参 |

`db.session()` 的默认连接由本次 Action 所属数据库决定，不取配置目录中第一个数据源。没有 `connect/open/commit/rollback/close` 接口，脚本不能控制事务边界。显式额外数据库连接保留 API 位置，跨库连接及跨库写入尚未实现。

新 Action 查询**不自动继承普通查询工具的表、字段、行权限和脱敏规则**，也不提供 `my.*`。管理员编写、审核并发布 Action，由脚本根据可信身份显式检查角色、数据归属和业务状态；UPDATE/DELETE 应再次包含关键归属与状态条件并检查 rowsAffected。普通查询工具仍执行原查询策略。证券示例的员工、账户与客户公共库已包含显式业务权限检查。

所有 SQL 必须直接写为 `query/queryOne/update` 的字符串字面量，使用 `:name` + 参数对象，或 `?` + 参数数组；不混用两种绑定。重复命名参数只绑定一次。值只接受 null、字符串、布尔和安全范围内有限数值，不接受对象、数组值或 undefined。SQL 结构在保存时静态抽取、验证；表名与列名写在 SQL 中，不能通过参数传入。动态 SQL、SQL 变量/拼接/普通模板字符串、方法别名和方括号调用不可执行数据库操作。当前 Action 不开放 qiuer 的动态 SQL 指令、数组展开、表达式参数和 queryTable/batch 等扩展。

`update` 不允许 DDL、事务控制或多条 SQL。UPDATE/DELETE 必须有 WHERE，禁止 REPLACE、RETURNING 和冲突处理；网络库还不支持 DML WITH、UPDATE FROM、UPSERT、DELETE ORDER/LIMIT 等形式。SQL 经现有 AST 与驱动适配层执行，写账号应只获得必需表的权限。

BIGINT、DECIMAL、NUMERIC 作为字符串返回；其他超出安全整数范围的整数也返回字符串。若驱动已把精确小数转换成浮点，拒绝返回，需在 SQL 中将字段转为文本。脚本应只返回业务所需数据。

每次调用创建独立 VM，入参及 context 深度冻结。正常返回才提交；未捕获 JS 异常、SQL 异常或超时均回滚，SQL 失败被 catch 后也不能提交。普通业务异常若被脚本主动 catch，则由脚本处理；需要不可恢复拒绝时仍可使用兼容的 `deny(message)`。总时限 5 秒（包括连接建立），最多 50 条 SQL，每次 SELECT 最多 1000 行，结果最多 1 MiB。数据库调用同步执行；已完成 Promise 可解包，rejected/pending Promise 拒绝，没有定时器事件循环。顶层 undefined 返回 null。没有网络、文件、进程或 npm 能力。

## 公共函数库

公共库保存在 `management/action-libraries/<name>.yml`，需要 `config.read` 和 `action.write`。虚拟路径为 `./<name>`，可加 `.js`，不解析文件系统、URL 或 npm 包。

```yaml
name: employee
title: 员工身份检查
description: 校验在职员工
script: |
  exports.operator = function(context) {
    const conn = db.session();
    const user = conn.queryOne(
      "SELECT employee_id, status FROM employees WHERE employee_id=:userId",
      {userId: context.userId}
    );
    if (!user || user.status !== "active") throw new HttpError(403, "员工不存在或已停用");
    return user;
  };
```

```js
const { operator } = require("./employee");
function main(params, context) {
  const user = operator(context);
  return {employeeId: user.employee_id};
}
```

支持 `exports.name`、`module.exports`，require 参数必须为静态相对路径。CommonJS 循环依赖遵循部分 exports 语义。保存时检查语法、依赖和静态 SQL；CJS 导出函数是否存在由运行时确定。被引用的库不能删除。只装载本次动作可达的库，模块内不按单个导出裁剪，模块变量不跨调用保留。初始化可以取得 session，但不能执行 SQL，即使捕获初始化 SQL 错误仍会失败。

不可变编译结果按数据库与完整源码缓存，最多 128 项、8 MiB；源码变更使用新结果。公共库与动作共享身份、事务、SQL 条数和期限。库更新提高 revision，并使旧确认预览失效；即使直接改 YAML 未提高 revision，代码指纹变化也拒绝旧确认。已完成幂等回执仍复用。配置克隆同时复制公共库，并检查可达 SQL 与目标库结构。

## 旧脚本兼容与证券迁移

已有 `run(args, ctx)`、ES Modules 和 `db.one/db.all/db.exec`、`my.one/my.all` SQL 标签模板继续通过旧路径执行；`my.*` 仅在旧路径继续应用原查询策略。新 `main` 脚本及其库使用 CommonJS，不能混用两套契约。

证券案例包的 18 个 Action 和 3 个公共库保存在 `examples/securities/actions.yml`。新库使用通用配置导入器，已有库通过管理界面或管理 MCP 更新相应 Action 与公共库；更新提高配置 revision 并使旧预览失效。

实际迁移前会备份逐库管理配置，通过 revision 检查一次保存全部脚本；重复迁移无变更。不提交证券业务数据，不重装视图或重新授权账号。源码升级后需构建并重启 API/MCP 服务，使新脚本由新执行器运行。

## 动作 AI 助手

助手按当前草稿入口选择契约：main 使用 db.session()、静态 SQL 字符串与 CommonJS，旧 run 保留标签模板路径，不自动迁移权限语义。两条路径均检查可达公共库中的读取，拒绝相对已保存动作新增或修改绕过普通查询策略的 SELECT；新读取需手工编写并审核。建议仅更新草稿，不自动保存或执行。assist_action 暂不暴露为管理 MCP 工具，外部客户端可通过动作保存与回滚试运行完成编写验证。

admin-api 的 `DATASCOPE_AI_ENDPOINT` 可填写以 `/v1` 结尾的网关基址或完整的 Chat Completions 端点，模型需支持 JSON 对象响应。`DATASCOPE_AI_MODEL` 选择模型，`DATASCOPE_AI_KEY` 仅配置在服务端，`DATASCOPE_AI_TIMEOUT` 默认为 120s，限制等待首个有效输出和后续连续空闲的时间。客户端请求流式响应，每段正文或推理增量都会重新计时。助手在流结束并校验完整 JSON 后返回建议。兼容完整 JSON 对象外层的 Markdown 代码块；流已明确返回 `finish_reason=stop` 时，允许正常 EOF 代替 `[DONE]`，并处理末尾省略空行的完整事件。缺少 stop、输出截断、过滤、工具调用替代正文、无正文、响应结构或建议字段错误分别返回可诊断原因，不返回提供商原始正文。`finish_reason=length` 时需提高提供商输出 token 上限或缩短需求；格式错误需核对模型的 JSON 输出能力。如果错误仍显示 30s，检查部署环境是否显式设置了旧值；改为 `DATASCOPE_AI_TIMEOUT=120s` 并重启 admin-api（容器环境变量变化时重新创建容器）。首段输出更慢时可进一步调整。空闲时限不是整次请求总时限，入口代理需覆盖完整生成及可能的一次自动修正；仓库 Docker 配置为管理调用预留 600s，外层网关也需相应配置。使用外部模型时，动作草稿与选定的结构元数据会发送到该端点。服务端记录模型、耗时、状态和提供商返回的 token 用量。

控制台通过管理接口 `assist_action` 发送数据库标识、revision、已保存的动作名（新增时省略）和 assistant。draft 模式允许提交当前编辑中的定义，test 使用已保存定义生成示例，diagnose 用当前平台账号的 request_id 查找回滚回执。参数校验失败可从提交给平台的参数重建诊断。动作编辑器右上角的 AI 辅助打开右侧对话抽屉，展示公开的方案摘要、正文、请求状态与耗时；当前没有模型工具调用，不将后台请求伪装成模型工具调用。draft 可传 history（最多 12 条 user/assistant 消息，每条最多 100000 字节，总上下文仍限制 250000 字节），最近六轮成功对话作为非可信上下文传入，当前草稿为准。响应可包含 summary（公开方案摘要，不是内部推理）和 explanation。用户比较参数与脚本差异，选择采纳项，再确认应用到草稿；保存仍为独立操作。停止生成或草稿变化会丢弃未完成结果。助手不会调用脚本、保存或发布。生成建议经过参数、模块和静态 SQL 校验。若参数或脚本校验失败，且原始上下文、建议与错误反馈合计不超过 250000 字节，最多再请求模型修正一次；保留原系统契约与当前草稿，修正后重新校验参数、模块、静态 SQL 和新增特权读取。不会执行脚本，不重试提供商错误，也不放宽权限校验。动作 SQL 使用统一解析器语法子集，不支持 PostgreSQL 的 `ILIKE` / `NOT ILIKE`；可使用 `LOWER(column) LIKE LOWER(${args.pattern})` / `NOT LIKE`，并在目标数据库验证排序规则与 Unicode 大小写行为。仍然校验失败时接口返回“AI 建议未通过动作参数或脚本校验”及底层具体原因，不返回可应用的建议。可将原因补充到编写需求中重新生成，或手工修改草稿。业务归属语义仍需作者审查并以不同身份试运行。

其他业务场景可向应用服务注入同一个 `internal/ai.Client`，用自己的权限、提示词和输出类型调用 `Complete(ctx, ai.Request{Messages: ..., JSONObject: true})`，再校验返回内容。普通文本可省略 JSONObject。测试中用 `ai.Func` 实现固定或失败响应。术语提取的测试调用方位于 `internal/platform/terms_test.go`。

离线验收使用 `go test ./internal/ai ./internal/action ./internal/platform`。浏览器闭环还需先运行 `npm --prefix web run build`，安装 Playwright 与 Chromium，设置 `DATASCOPE_ASSISTANT_BROWSER_NODE=node` 后执行 `go test ./internal/platform -run '^TestAssistantBrowser$' -count=1 -v`。可用 `DATASCOPE_PLAYWRIGHT` 指定模块路径、`DATASCOPE_CHROMIUM` 指定浏览器路径。测试启动隔离的 admin-api 和 SQLite 夹具，使用模拟模型。

## MCP 调用和人工确认

```json
{"name":"rename_item","arguments":{"id":1,"name":"新名称"},"request_id":"unique-operation-uuid"}
```

`list_actions({})` 列出可用操作，`describe_action({name})` 返回单个动作的参数 Schema。`invoke_action` 返回 `done`、`denied` 或 `confirm_required`。

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

request_id 在同一 MCP 运行实例内按数据库、发起账号隔离。相同 ID、操作和参数复用结果；同一 ID 换操作、参数或身份会拒绝。内存互斥锁与状态比较保护并发请求，结果未知的请求在该实例内不会自动重放。

回执、确认令牌及执行结果只保存在内存，不写入管理库、YAML 或 audit 表。MCP 重启即清空，不提供跨重启防重保证；重启后重试旧请求可能重新执行，应先核对业务数据。缓存上限为 10000 条、64 MiB，达到上限拒绝新增，不通过自动淘汰回执破坏运行期防重。

MCP 进程持有唯一内存缓存，API 的确认页和平台试运行通过权限为 0600 的本机 Unix socket 访问它。两个服务的 ACTION_STATE_SOCKET 必须一致，默认是全局管理库同级 runtime/actions.sock；多个独立部署使用各自路径。socket 不包含回执数据。MCP 内存服务不可用时，Action 操作报错，不回退到另一个独立缓存。平台账号仍不能代替业务账号正式执行或确认。

每条 SQL 的内容、绑定参数、影响行数及错误继续异步进入 audit.db。审计不可写不阻止业务提交；它只用于追踪，不承担幂等控制。旧版本 ACTION_RECEIPT 表不再读取或写入，新库不创建该表；已有逐库表保留作历史备份。

控制台「试运行」始终回滚，包括 confirm:false 的操作，显示 SQL 审计和执行预览。管理 MCP 使用 `datascope_manage` 的 `{action:"save",args:{resourceType:"action",database_id,database,write_password?}}` 保存 action、写连接和启用状态；使用 `datascope_inspect` 的 `{action:"try",args:{resourceType:"action",database_id,name,arguments,request_id,business_user_id}}` 试运行，始终回滚。读取当前配置与 revision 后再保存。

工具区的「动作工作台」独立于代码编辑。在回滚试运行页签，先手动获取动作列表，再选择动作查看完整参数 JSON Schema、约束和人工确认标志，填写 JSON 参数与业务身份后试运行。列表、说明和试运行分别展示实际请求参数、返回 JSON 与耗时；结果展示返回值及 SQL 写入计划。`request_id` 可编辑，同一 ID 重用历史预览，修改参数或身份会自动生成新 ID，也可点击“新请求”重新预览。列表与说明分别通过平台 `list_actions` / `describe_action(name)` 返回与业务 MCP 相同格式的元数据，不返回脚本；需要 `action.write`，未开启 MCP 或写入时仍可查看。试运行沿用 `try_action`，另外要求 `impersonate` 和已启用写入，任何 confirm 设置均回滚；真实业务调用页签通过本人业务认证和业务 MCP 的 `invoke_action` 发起正式操作并处理人工确认，见下文。管理 MCP 可用 `datascope_query({action:"list",args:{resourceType:"action",database_id}})` 获取列表，再用 `datascope_query({action:"describe",args:{resourceType:"action",database_id,name}})` 读取参数说明。

SQLite 已有事务、权限、幂等、确认和并发端到端回归；网络库使用共享连接和 AST 编译，并有各驱动 SQL 编译/绑定测试。接入真实 MySQL、PostgreSQL、Oracle、达梦写账号后，仍需在目标库验收其账号权限、字段精度、锁和超时行为；测试未配置实际写账号时不代表已做真实库写入验收。

证券演示库已提供可显式安装的 18 个操作，见 [完整案例](../examples/securities/docs/actions.md)；涉及的结构、状态值和发布步骤见 [发布清单](../examples/securities/docs/actions-release.md)。

## 工作台正式调用

动作工作台提供“试运行（回滚）”与“真实业务调用”。前者使用平台 action.write + impersonate 权限，始终回滚；后者仅允许平台动作编辑者使用本人业务认证访问真实业务 MCP，平台身份不会变成业务身份。

- NONE：使用库配置的匿名身份和能力，不能凭空获得 action；人工确认也不能冒充本地账号。
- ACCOUNT：复用既有 /auth/databases/{id}/token 端点，账号密码换取当前库 Token；也可粘贴本人已有 Token。
- OAUTH：内置 OAuth 复用既有注册、PKCE 授权码和 Token 端点，独立窗口由本人登录；企业 OAuth 使用本人已取得的业务 Bearer Token，不新增企业认证协议。

Token 仅保留在当前页面内存，不进入 localStorage、配置、请求追踪展示或日志。离开页签销毁认证状态。平台会话只控制工作台入口，业务 MCP 每次独立验证 Token、数据库、工具开关和 action 能力。

正式调用需要明确点击并确认。confirm_required 展示回滚 preview 和现有确认页链接，由本人打开确认页登录执行；工作台不调用确认接口。查询结果使用同一 request_id 调用 invoke_action，并指定 result_only:true；只查询当前业务身份的内存回执。网络故障或 MCP 重启后缺失回执返回 unknown，用户应核对审计和业务数据，工作台不自动重试写入。

## 动作总开关

动作列表表头和 MCP 配置页共用同一总开关（`write_enabled`）。`configure_capability` 的 actions 组原子启停 `list_actions`、`describe_action`、`invoke_action`，保留动作内容及发布状态。需 `action.write`，网络库需独立写入账号。关闭后正式执行及回滚试运行均被拒绝，管理端仍可编辑和发布草稿。

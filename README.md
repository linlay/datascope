# DataScope 数据库访问管理平台

Go + React 实现的数据库访问平台，支持 SQLite、MySQL、PostgreSQL、Oracle 和达梦 DM8 业务数据源，内置平台用户、数据库直接授权、SQL 权限改写、独立审计库，以及管理系统自身的 MCP 与逐库数据库 MCP。

## 启动

需要 Go 1.26、Node 22+。

```sh
cp .env.example .env
# 设置 ADMIN_PASSWORD（至少 12 字符），首次初始化平台管理员。
# 设置 OAUTH_DEMO_PASSWORD（至少 12 字符），首次初始化证券 demo 员工模拟账号。
make init          # 已有演示业务库时跳过；不会覆盖已有文件
make web-install
make build
make run-api       # 终端一，默认 8081；发布 OAuth 和管理 MCP
make run-mcp       # 终端二，默认 8080；提供数据库 MCP
make run-web       # 终端三，默认 8082；管理控制台
```

首次使用 `ADMIN_USERNAME`（默认 `admin`）与配置的 `ADMIN_PASSWORD` 登录。密码使用带随机盐的 PBKDF2-SHA256 保存。初始化完成后，这两个环境变量不会覆盖已有账号；密码通过用户管理重置。后台会话使用 HttpOnly、SameSite Cookie，密码重置和用户禁用会撤销会话，旧 OAuth JWT 也会失效。

两个 Go 服务必须使用相同的 `MANAGEMENT_DB_PATH`、`AUDIT_DB_PATH` 和 `DATABASE_ALLOWED_ROOTS`。更改监听端口时同步修改 `MCP_RESOURCE_URL`、`MCP_SERVER_URL`、`OAUTH_ISSUER`、`ADMIN_API_URL`。外部部署使用 HTTPS；HTTPS 反向代理后设置 `SESSION_COOKIE_SECURE=true`，或使用 HTTPS `OAUTH_ISSUER` 的默认推断。

可使用 `SQLITE_PATH` 指定初始 SQLite 业务数据库；仅接入网络数据库时设为显式空值，允许空数据源启动，再从控制台添加。多数据库配置、SQL 子集、达梦平台说明、Linux 构建与真实库验收见 [多数据库接入说明](docs/databases.md)。

## 模块与持久化

```text
internal/
  management/       平台用户、会话、直接授权、配置记录与版本、审计 outbox
  guard/            统一授权检查与可信身份上下文
  rewrite/          AST 解析、表字段限制、行权限注入、参数检查
  store/            只读数据库执行、结构读取、分页、超时与行数上限
  audit/            请求、改写、执行、权限检查及操作审计
  application/      Web 与管理 MCP 共用的领域动作
  adminapi/         受保护的后台 HTTP API
  managementmcp/    管理平台自身的 MCP 工具
  mcpserver/        逐库数据库 MCP、知识读取、配置生成的业务工具
  oauth/            平台账号 OAuth 授权、资源隔离、JWT 校验
  platform/         启动装配与集成测试
```

默认内部状态：

| 文件 | 职责 |
| --- | --- |
| `data/management.sqlite` | 用户、会话、授权、业务身份、数据库与 MCP 配置、知识、版本、OAuth 状态 |
| `data/audit.sqlite` | 原始 SQL、改写 SQL、执行 SQL、绑定参数、执行结果状态与管理操作 |
| `data/oauth/signing.pem` | OAuth 私钥，独立文件保存 |
| `data/management.sqlite.credentials.key` | 数据库连接密码加密密钥，首次直接保存密码时自动生成，须与管理库一同备份 |

业务数据库只读连接。`DATABASE_ALLOWED_ROOTS` 指定允许登记的目录，macOS/Linux 多个目录用 `:` 分隔；默认 `data`。管理库、审计库及其符号链接、硬链接不能注册为业务数据源。原始查询也禁止 ATTACH 和修改连接状态的 PRAGMA，避免从已授权业务库访问内部状态。

## 用户与数据库授权

没有角色表或角色继承。每个用户直接关联数据库，并授予能力：

| 权限 | 能力 |
| --- | --- |
| `config.read` | 查看连接和配置，使用配置管理页面 |
| `config.write` | 修改数据库名称 |
| `policy.write` | 修改表、字段、行权限策略 |
| `mcp.write` | 配置 MCP 地址、基础工具与业务工具 |
| `knowledge.read` | 通过数据库 MCP 检索、读取已发布知识；还需 `query` |
| `knowledge.write` | 编辑、发布知识 |
| `query` | 以可信业务身份改写并执行查询 |
| `raw_query` | 全量只读查询及表数据浏览，绕过行字段策略，需单独授予 |
| `audit.read` | 查看当前数据库审计 |
| `grant` | 分配、撤销自身拥有的数据库权限 |
| `impersonate` | 在改写工作台模拟业务身份 |

初始化管理员是系统恢复入口，拥有全部能力。普通用户不能自行创建平台管理员，不能授予自身没有的能力，不能修改业务身份绑定。平台管理用户与业务员工账号完全独立。每个数据库可使用 DataScope 模拟的独立员工认证域；同名工号在不同数据库中不共享密码、业务身份和权限。演示库中的角色只决定演示业务行权限。

在“库配置 → 用户配置”中分开管理平台用户和当前数据库的业务员工账号。业务账号的**业务身份编号**是该数据源 `${userId}` 的可信值，例如证券演示库的 `employees.employee_id`。它不需要对应的平台账号，也不继承平台管理权限。平台用户的数据库授权用于后台管理及控制台；管理员可在有 `impersonate` 权限的改写工作台模拟业务身份。独立业务认证的 MCP 测试请通过业务连接器完成真实员工登录。

新增数据库仅需填写显示名称及连接信息，ID 自动生成。网络数据库直接输入密码即可，密码通过独立密钥加密保存，不会写入配置历史或返回给客户端；旧环境变量引用继续兼容。新增数据库默认关闭 MCP、禁止所有表。先配置访问策略，再打开 MCP。查询用户只获得数据库名称等最小目录信息；连接路径与策略配置只向具备 `config.read` 的用户提供。配置页通常需要同时授予 `config.read` 和对应修改权限。

## 自动侦查、映射与结构变化

添加数据库会自动建立完整表字段快照，提取表和字段注释、类型、主键、非空、默认值、外键及索引名称；自动生成全部表的访问策略，默认禁止所有表，并固定当前字段列表。控制台依次为「库配置」「表配置与权限」「业务知识」「SQL 控制台」「表数据」「MCP 配置」。库配置包含基本连接信息、显示名称、用户配置和操作日志；表配置与权限统一提供字段开放选择、行过滤、展示名称映射和人工说明。物理 SQL 名称保持不变。

管理 API 启动后自动为旧数据源建立基线，每隔 `DISCOVERY_INTERVAL`（默认 `5m`，设为 `0s` 关闭）检查结构；无需保持浏览器打开。「检查数据库变化」可随时检查。检测到结构差异只保存待确认的对比，不会立即更改已登记结构、人工说明或访问策略；页面提示后在对比弹窗确认，才采用新结构。确认时再次读取源结构，若数据库又有变化或配置版本已更新，拒绝应用过期对比。无变化时不新增结构版本，也不使正在编辑的配置过期。

确认采用新结构后，新增表默认禁止，新增字段不进入已有字段白名单，删除字段从白名单移除，删除表从当前策略移除；字段全部删除时关闭该表。显式勾选「包含未来新增字段」才会让该表后续字段自动可见。表或字段改名按删除和新增处理，不猜测关联。

数据库注释与人工说明独立保存：未覆盖的说明随源注释更新；人工值（包括显式空字符串）默认保留。数据库注释变化且与人工值不同时，报告同时展示旧值、新值、人工值并标记冲突。可以在变更对比弹窗逐项选择保留人工说明或采用数据库新注释，也可在说明编辑中取消覆盖。映射与有效说明同时用于数据浏览和数据库 MCP 的结构说明。权限规则、行过滤模板和业务工具不会被扫描器自动重写；结构变化导致的自定义 SQL 失效需管理员调整，期间结构检查和分页浏览仍可使用。

扫描使用只读连接，不执行全表 COUNT，不复制业务数据。通过「浏览数据」按页读取各表全部内容；全量数据浏览需 `raw_query`。本功能比较结构和注释，不比较业务记录的增删改。扫描失败保留上次完整快照，自动扫描失败会显示错误；最近 20 次结构变化报告可在页面切换查看，管理库仍按已有机制保留配置历史和审计。映射编辑与扫描使用配置版本检查，避免覆盖其他窗口的修改。

Web 与管理 MCP 共用管理动作：`get_discovery`（`config.read`）、`discover_database` 和 `save_annotation`（另需 `config.write`），以及确认应用的 `apply_discovery`（还需 `policy.write`）。管理 MCP 分别使用 `datascope_query` 的 `discovery.get`、`datascope_manage` 的 `discovery.refresh` 、`discovery.apply` 和 `annotation.save`，参数见管理连接器参考。

## 每个数据库独立的业务认证

数据库的 `authentication` 配置选择 MCP 使用的账号域：

```json
{"authentication":{"type":"local","label":"证券 demo 员工认证","employee_directory":true}}
```

- `local`：DataScope 模拟当前数据库的员工认证。账号单独存入 `business_accounts`，以 `(database_id, username)` 隔离，密码独立哈希；业务库仍只读，不要求业务库自行提供账号表。新登记数据库默认选择该模式，初始账号为空。
- `employee_directory: true`：额外校验当前业务库 `employees` 目录的工号、员工编号与在职状态，适用于证券 demo。其他数据库通常关闭，由其业务账号绑定对应 SQL 策略需要的业务身份编号。
- `platform`：旧配置兼容模式，业务 MCP 仍使用平台用户及原业务身份绑定。未配置 `authentication` 的已有非证券数据源保留旧行为；可在“库配置 → 用户配置”中显式迁移。

平台管理员可以为每个数据库分别创建、停用业务账号、设置业务能力或重置密码，也可以通过管理 MCP 的 `business_account.list/save` 操作维护。业务账号最多获得 `query`、`knowledge.read` 和单独授予的 `raw_query`；它不能登录管理后台或调用管理 MCP。管理业务账号及切换认证来源仅限平台管理员。

每个 local 数据库有独立 OAuth issuer：`{OAUTH_ISSUER}/auth/databases/{database_id}`。业务 MCP 的资源发现元数据返回该 issuer，客户端随后发现其授权、注册、令牌端点并跳转该数据库的员工登录页。管理 MCP 始终使用平台根 issuer。旧客户端仍从根授权入口发起业务授权时，会跳转相应数据库的员工认证入口。

令牌绑定精确 MCP resource、数据库账号域、账号版本和认证配置版本；密码重置、账号停用、业务能力或身份变更、认证来源切换都会使旧令牌和刷新凭据失效。切换来源后需在客户端重新授权连接器。相同工号可以存在于两个数据库甚至平台用户中，三个账号分别认证。

首次启动新版服务时，若初始业务库包含证券 demo 员工目录且尚未配置认证来源，设置 `OAUTH_DEMO_PASSWORD`（至少 12 字符），服务会一次性为目录员工建立证券库的独立模拟账号并启用目录校验。E0001 对应业务身份 1。此密码仅用于首次创建，不覆盖已有业务密码；以后在当前数据库的业务账号列表中重置。新增其他业务数据库会拥有另一套空的模拟账号域，不复制证券库员工或密码。当前内置来源为逐库模拟认证和旧平台兼容模式；外部企业认证提供方尚未接入。

## 两类 MCP

### 管理 MCP

默认端点：`http://127.0.0.1:8081/mcp/admin`，使用平台账号完成 OAuth 授权。

管理入口收敛为三个工具，统一使用 `{action,args}`，由 `args.resourceType` 指定资源：

| 工具 | 用途 | 支持的资源 |
| --- | --- | --- |
| `datascope_query` | 查询配置、用户、授权、知识和审计 | database、policy、user、grant、knowledge、audit |
| `datascope_manage` | 创建与修改管理资源 | database、policy、mcp、user、grant、knowledge |
| `datascope_inspect` | 连接测试、策略校验、SQL 改写预览 | database、policy |

例如读取数据库配置：

```json
{"action":"get","args":{"resourceType":"database","database_id":"default"}}
```

`deploy/connectors/datascope-admin/` 提供独立管理连接器，附带 `datascope-admin` skill 和数据库、策略、用户授权、知识、审计操作参考。在管理界面的“库配置 → 用户配置 → 管理连接器”点击“下载连接器 ZIP”，下载包中的 MCP 地址由部署的 `OAUTH_ISSUER` 配置生成；通过完整连接器包导入并挂载到 Agent，完成管理端 OAuth 授权后使用。下载接口 `GET /api/connectors/datascope-admin.zip` 需要登录，不包含任何账号凭据。使用源码目录打包时，需按实际部署地址修改包内 `mcp.json`。只导入 MCP 地址不会携带 skill。打包可在仓库根运行 `(cd deploy/connectors && zip -r /tmp/datascope-admin.zip datascope-admin)`。

工具 schema 保留简短的动作和资源枚举；资源与动作矩阵、详细参数和管理流程由配套 skill 描述，服务端严格拒绝不支持的组合、无关参数与错误类型。原有 25 个独立管理工具不再由管理 MCP 暴露；其他 MCP 客户端也需改用上述调用结构。Web 管理端的原业务动作接口保持不变。

管理 MCP 通过业务动作维护管理 SQLite，不提供对内部库执行任意 SQL 的能力。Web 控制台与 MCP 复用同一应用服务，继续按原动作检查权限并记录审计。配置修改需要 `args.database.revision`，知识更新需要 `args.revision`，旧版本不能覆盖新配置。查询与诊断不保存业务配置，但仍记录审计；策略校验仍需 `policy.write`。

### 数据库 MCP

默认地址为 `http://127.0.0.1:8080/mcp/{mcp_path}`，保留原有分库地址及默认库 `/mcp` 入口。`admin` 为保留路径后缀。

每个数据库独立配置基础工具：`list_tables`、`describe_table`、`query`、`raw_query`、`customer_scope`、`search_knowledge`、`read_knowledge`。前五个沿用已有工具，其中 `customer_scope` 仅适用于证券演示库。

HTTP OAuth 的 audience 精确绑定完整地址，管理端令牌与不同数据库的令牌不能混用。业务 MCP 按数据库认证来源重新检查当前业务账号、能力和工具启停；管理 MCP 检查平台账号及管理授权。已有连接不能保留已撤销权限。

`header` 模式只用于会剥离外部身份头的可信网关；`X-User-Id` 现在表示**平台用户 ID**。`none` 模式和 stdio 需要显式 `MCP_DEFAULT_USER_ID`，同样表示平台用户；stdio 使用 `MCP_DATABASE` 选择数据库。这两种方式只用于旧平台兼容认证；独立 local 业务账号域要求 OAuth。

## 配置生成业务查询工具

在“MCP 配置与测试”的业务工具 JSON 中设置，或通过管理 MCP 的 `datascope_manage`（`action:"save"`、`args.resourceType:"mcp"`）更新 `args.database.business_tools`：

```json
[
  {
    "name": "find_customer",
    "description": "按客户编号查询当前用户可见的客户",
    "sql": "SELECT customer_no,name FROM customers WHERE customer_no=:arg_customer_no",
    "parameters": {
      "customer_no": {"type": "string", "description": "客户编号"}
    }
  }
]
```

调用参数为 `{"customer_no":"C000001"}`。支持 string、integer、number、boolean，参数必须与定义完全匹配。模板中的业务参数必须使用 `:arg_名称`，与 `${userId}`、`${asOf}` 等可信上下文变量隔离。SQL 经过 AST 权限改写后，以数据库绑定参数执行；不会将参数拼入 SQL。

工具定义保存时验证名称、参数与 SQL。业务工具自动出现在对应数据库 MCP 中，删除定义即停用；MCP 总开关继续生效。

## 业务知识与渐进式披露

“业务知识”中编辑名称、摘要、正文、关联表与发布状态。建议正文记录表关联、关联基数、金额单位、统计口径、去重方式、查询步骤与示例 SQL。

模型使用过程：

```text
search_knowledge("客户月末资产") → 名称、摘要、ID、版本
read_knowledge(id)              → 业务口径、关联说明与 SQL 示例
describe_table(...)            → 按需查看字段
query(...)                     → 权限改写并执行
```

检索采用 SQLite FTS5 trigram，并补充短文本子串匹配。每次最多返回 50 条摘要，避免一次加载全部正文。草稿不对查询用户公开。关联表被禁用或设置字段白名单时，查询侧保守隐藏该知识，避免正文泄露受限结构。知识只提供业务指导，不能扩大数据库访问权限。

## 审计与一致性

SQL 在改写和执行前同步写入起始记录，审计不可用时拒绝执行。记录包括 request ID、平台用户、业务身份、入口、工具、策略版本、原始 SQL、改写 SQL、执行 SQL、参数、时间、返回行数、截断和错误。

覆盖普通查询、改写预览、全量只读查询、分页浏览自动生成的 SQL，以及 `deny_unless` 的前置检查。SQL 解析失败、权限拒绝、超时也有记录。MCP HTTP 层在认证前记录 SQL 请求，因此撤销令牌后的拒绝请求仍可追踪。结果数据正文不保存；密码、会话令牌和 OAuth 凭据不进入操作审计。

管理变更与审计 outbox 在管理库同一事务提交，后台重试投递到审计库；事件 ID 去重。数据库配置和知识同时保留历史版本。完成审计独立于请求取消进行有限重试；长期未完成的事件显示为结果未知，不能据此判定查询成功。当前不自动删除审计，备份和保留周期由部署方安排。

## 旧版本迁移与备份

首次启动自动导入现有逐库 YAML；无逐库配置时，沿用旧 JSON 或初始改写配置。导入后 **SQLite 是唯一配置来源**，后续不再读取手工编辑的 YAML。旧文件保留，便于核对和回退；运行中的配置修改请通过 Web 或管理 MCP。

旧 OAuth 客户端注册可迁移到管理库；旧员工身份的授权码、刷新令牌不能映射成平台授权，需要重新登录。旧演示统一密码与无密码员工模拟不用于新平台。

升级前备份配置目录、业务数据、旧 OAuth 状态及私钥。升级后备份两个内部 SQLite、OAuth 私钥及数据库凭据加密密钥；运行中备份 SQLite 使用 SQLite backup API 或包含一致 WAL 状态的方式，不能只复制正在写入的主文件。多实例并发读写仅面向同机共享 SQLite 文件，不作为跨主机高可用方案。

## 业务演示与验证

证券演示库仍支持客户、资金账户、证券账户、交易与持仓等权限改写。结构和说明见 [DDL](sql/schema.sqlite.sql)、[数据字典](docs/data-dictionary.md)、[业务权限说明](docs/permissions.md)。`make init-shop` 可生成三张表的小型商店库。

```sh
make test          # 连接器 VIEW 回归测试 + Go 全部测试 + 前端 TypeScript 检查
make build         # 两个服务器、数据生成器与前端构建
```

新增集成测试使用隔离临时库，覆盖后台登录、越权与身份冒用、跨资源令牌、权限即时撤销、密码重置撤销 JWT、真实管理 MCP、业务工具参数注入、知识摘要与全文披露、并发版本冲突、内部库隔离、审计 outbox、审计失败时拒绝执行。旧证券演示验收通过独立测试运行，不作为后台公共接口开放。

## 标准连接器

`deploy/connectors/datascope/` 提供业务查询连接器，`deploy/connectors/datascope-admin/` 提供携带管理 skill 的管理连接器；两者独立授权。标准 MCP 连接器配置中，`connector.json` 定义连接器信息，`mcp.json` 定义服务地址与传输方式。使用前按实际部署地址修改 `mcp.json`；支持 MCP 的客户端可直接配置服务地址并完成 OAuth 授权。

### 业务查询连接器的 SQL VIEW

业务查询连接器附带 `sql` 显示视图（`view.json`、`views/`），供 Agent Platform 在对话中渲染可修改变量的查询结果。模型在回复中写不带 `query=` 的数据块，正文给出 SQL 原文和变量定义；SQL 中的变量用裸写的 `{{name}}` 占位：

````markdown
```view key=datascope/sql
{"title":"客户日终余额",
 "sql":"SELECT c.customer_no,c.name,b.fund_account_no,b.closing_balance/100.0 AS closing_balance_yuan FROM customers c JOIN daily_balances b ON b.customer_no=c.customer_no WHERE c.customer_type={{ctype}} AND b.trade_date={{day}} AND b.closing_balance>={{min}} ORDER BY b.closing_balance DESC",
 "variables":{
   "ctype":{"type":"string","label":"客户类型","default":"INDIVIDUAL","options":[{"value":"INDIVIDUAL","label":"个人"},{"value":"CORPORATE","label":"机构"}]},
   "day":{"type":"date","label":"交易日","default":"2026-10-20"},
   "min":{"type":"integer","label":"最低余额（分）","default":0}},
 "maxRows":200}
```
````

- 变量类型为 string、integer、number、date（`YYYY-MM-DD`）和 boolean，每个变量必须有 `default`；`options` 以下拉框展示可选值。
- 视图打开后立即用默认值填入 SQL 并通过 `query` 工具执行；用户修改变量后重新填写并查询。输入留空表示使用默认值，“恢复默认”清除全部修改。修改的值不保存，历史中只保留原始块。
- 视图按类型生成 SQL 字面量：字符串加单引号并转义，负数加括号，布尔写为 1/0；占位符不能写在引号或注释里，未声明的占位符直接报错。最终 SQL 仍由服务端解析并按权限改写，视图只调用 `query`，不使用 `raw_query`。
- 数据库需启用 `query` 工具，并在该数据库的独立业务账号域中为员工账号配置业务身份。连接器 ID 当前为默认库的 `datascope`；按库拼装的连接器下载尚未提供。

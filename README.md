# DataScope 数据库访问管理平台

Go + React 实现的 SQLite 数据库访问平台，内置平台用户、数据库直接授权、SQL 权限改写、独立审计库，以及管理系统自身的 MCP 与逐库数据库 MCP。

## 启动

需要 Go 1.26、Node 22+。

```sh
cp .env.example .env
# 设置 ADMIN_PASSWORD（至少 12 字符），首次初始化平台管理员。
make init          # 已有演示业务库时跳过；不会覆盖已有文件
make web-install
make build
make run-api       # 终端一，默认 8081；发布 OAuth 和管理 MCP
make run-mcp       # 终端二，默认 8080；提供数据库 MCP
make run-web       # 终端三，默认 8082；管理控制台
```

首次使用 `ADMIN_USERNAME`（默认 `admin`）与配置的 `ADMIN_PASSWORD` 登录。密码使用带随机盐的 PBKDF2-SHA256 保存。初始化完成后，这两个环境变量不会覆盖已有账号；密码通过用户管理重置。后台会话使用 HttpOnly、SameSite Cookie，密码重置和用户禁用会撤销会话，旧 OAuth JWT 也会失效。

两个 Go 服务必须使用相同的 `MANAGEMENT_DB_PATH`、`AUDIT_DB_PATH` 和 `DATABASE_ALLOWED_ROOTS`。更改监听端口时同步修改 `MCP_RESOURCE_URL`、`MCP_SERVER_URL`、`OAUTH_ISSUER`、`ADMIN_API_URL`。外部部署使用 HTTPS；HTTPS 反向代理后设置 `SESSION_COOKIE_SECURE=true`，或使用 HTTPS `OAUTH_ISSUER` 的默认推断。

首次启动仍需要 `SQLITE_PATH` 指定的初始业务数据库。可继续使用证券演示库；后续在控制台登记其他普通 SQLite 文件。

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

初始化管理员是系统恢复入口，拥有全部能力。普通用户不能自行创建平台管理员，不能授予自身没有的能力，不能修改业务身份绑定。平台用户与证券演示库的员工、角色完全独立；演示库中的角色只决定演示业务行权限。

在“用户与授权”中创建用户，再为用户设置数据库权限和**业务身份编号**。业务身份是该数据源 `${userId}` 的可信值，例如证券演示库的 `employees.employee_id`；平台用户 ID 不直接用作业务身份。初始化管理员如需测试自己的改写查询，也应为自己设置业务身份。MCP 测试始终使用当前平台账号；模拟业务身份只在获授权的改写工作台提供。

新增数据库默认关闭 MCP、禁止所有表。先配置访问策略，再打开 MCP。查询用户只获得数据库名称等最小目录信息；连接路径与策略配置只向具备 `config.read` 的用户提供。配置页通常需要同时授予 `config.read` 和对应修改权限。

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

`deploy/connectors/datascope-admin/` 提供独立管理连接器，附带 `datascope-admin` skill 和数据库、策略、用户授权、知识、审计操作参考。在管理界面的“用户与授权 → 管理连接器”点击“下载连接器 ZIP”，下载包中的 MCP 地址由部署的 `OAUTH_ISSUER` 配置生成；通过完整连接器包导入并挂载到 Agent，完成管理端 OAuth 授权后使用。下载接口 `GET /api/connectors/datascope-admin.zip` 需要登录，不包含任何账号凭据。使用源码目录打包时，需按实际部署地址修改包内 `mcp.json`。只导入 MCP 地址不会携带 skill。打包可在仓库根运行 `(cd deploy/connectors && zip -r /tmp/datascope-admin.zip datascope-admin)`。

工具 schema 保留简短的动作和资源枚举；资源与动作矩阵、详细参数和管理流程由配套 skill 描述，服务端严格拒绝不支持的组合、无关参数与错误类型。原有 25 个独立管理工具不再由管理 MCP 暴露；其他 MCP 客户端也需改用上述调用结构。Web 管理端的原业务动作接口保持不变。

管理 MCP 通过业务动作维护管理 SQLite，不提供对内部库执行任意 SQL 的能力。Web 控制台与 MCP 复用同一应用服务，继续按原动作检查权限并记录审计。配置修改需要 `args.database.revision`，知识更新需要 `args.revision`，旧版本不能覆盖新配置。查询与诊断不保存业务配置，但仍记录审计；策略校验仍需 `policy.write`。

### 数据库 MCP

默认地址为 `http://127.0.0.1:8080/mcp/{mcp_path}`，保留原有分库地址及默认库 `/mcp` 入口。`admin` 为保留路径后缀。

每个数据库独立配置基础工具：`list_tables`、`describe_table`、`query`、`raw_query`、`customer_scope`、`search_knowledge`、`read_knowledge`。前五个沿用已有工具，其中 `customer_scope` 仅适用于证券演示库。

HTTP OAuth 的 audience 精确绑定完整地址，管理端令牌与不同数据库的令牌不能混用。工具调用时重新检查用户状态、数据库授权和工具启停；已有连接不能保留已撤销权限。

`header` 模式只用于会剥离外部身份头的可信网关；`X-User-Id` 现在表示**平台用户 ID**。`none` 模式和 stdio 需要显式 `MCP_DEFAULT_USER_ID`，同样表示平台用户；stdio 使用 `MCP_DATABASE` 选择数据库。

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

升级前备份配置目录、业务数据、旧 OAuth 状态及私钥。升级后备份两个内部 SQLite 和私钥；运行中备份 SQLite 使用 SQLite backup API 或包含一致 WAL 状态的方式，不能只复制正在写入的主文件。多实例并发读写仅面向同机共享 SQLite 文件，不作为跨主机高可用方案。

## 业务演示与验证

证券演示库仍支持客户、资金账户、证券账户、交易与持仓等权限改写。结构和说明见 [DDL](sql/schema.sqlite.sql)、[数据字典](docs/data-dictionary.md)、[业务权限说明](docs/permissions.md)。`make init-shop` 可生成三张表的小型商店库。

```sh
make test          # Go 全部测试 + 前端 TypeScript 检查
make build         # 两个服务器、数据生成器与前端构建
```

新增集成测试使用隔离临时库，覆盖后台登录、越权与身份冒用、跨资源令牌、权限即时撤销、密码重置撤销 JWT、真实管理 MCP、业务工具参数注入、知识摘要与全文披露、并发版本冲突、内部库隔离、审计 outbox、审计失败时拒绝执行。旧证券演示验收通过独立测试运行，不作为后台公共接口开放。

## 标准连接器

`deploy/connectors/datascope/` 提供业务查询连接器，`deploy/connectors/datascope-admin/` 提供携带管理 skill 的管理连接器；两者独立授权。标准 MCP 连接器配置中，`connector.json` 定义连接器信息，`mcp.json` 定义服务地址与传输方式。使用前按实际部署地址修改 `mcp.json`；支持 MCP 的客户端可直接配置服务地址并完成 OAuth 授权。

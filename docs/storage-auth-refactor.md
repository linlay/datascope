# 本地存储、认证与管理界面改造

本轮同时改造界面、管理存储与认证。业务库表结构与业务数据保持原状；DataScope 自有管理表发生了迁移，发布时必须携带新的管理目录。

本文记录当前实现。后续仅业务管理配置 YAML 化、knowledges/ 目录、Prompts 复用、克隆及取消模板/公共片段的最新约定见 [下一阶段方案](management-storage-plan.md)，尚未切换运行时存储。

## 文件布局与发布

```text
data/
  management.db                # 全局管理
  audit.db                     # 全局管理和认证的审计
  databases/
    default/
      management.db            # 证券库自己的管理数据
      audit.db                 # 证券库审计
      credentials.key          # 仅有加密连接密码时存在
    <其他数据库ID>/
      management.db
      audit.db
      credentials.key
  oauth/signing.pem            # 演示 OAuth 签名私钥
  securities_demo.sqlite3      # 已有业务文件路径保留，不改业务规范
```

新建的内部 SQLite 全部使用 `.db`；已有业务文件扩展名由业务自己决定。每个业务管理库仍叫 `management.db`，通过逐库目录与全局库区分。管理库与审计库禁止指向同一文件，内部库及其硬链接 / 符号链接不能作为业务数据源。

发布完整环境时，需要：

1. 业务数据库或外部业务数据库的独立迁移方案。
2. `data/management.db` 与完整 `data/databases/`，包括每个源的 `credentials.key`。密钥不进入 JSON 配置或日志。
3. 需要保留历史时，带上全局及逐库 `audit.db`；审计不可写不影响业务功能。
4. 演示 OAuth 的 `data/oauth/signing.pem`、部署配置和企业认证密钥环境变量。

先停止 API / MCP，再整体复制；如不停机，使用 SQLite backup / VACUUM INTO 生成一致性快照，不能只复制运行中 WAL 模式下的 `.db` 主文件。锁文件、`-wal` / `-shm` 是运行辅助文件，按所用备份工具的快照规则处理。

`ACTION_RECEIPT` 是防止重复写入的业务回执，必须随业务管理库迁移。它不属于可丢弃的审计；状态为 `running` 的未知执行结果不会自动重试。迁移后浏览器会话、授权码、刷新凭据需要重新登录 / 授权。

## 自动迁移与回退

```sh
make migrate-state
# 或指定包含旧 management.sqlite / audit.sqlite 的目录：
go run ./cmd/migrate-state -data-dir /path/to/data
```

API / MCP 启动也会自动执行迁移。先生成旧库的一致性快照，再重命名元数据字段并拆分源配置；原始 `.sqlite` 与旧密钥保留。跨进程文件锁防止两个服务同时初始化。迁移保留用户和业务账号 ID、密码哈希、授权、配置版本、知识、加密连接密码和 Action 回执。旧连接密码使用原密钥解密后按各库自己的密钥重新加密。

迁移前先备份原目录；回退旧版本时，停止新服务并使用原 `.sqlite` / 原密钥。迁移完成以后，新版本只使用 `.db`，之后产生的新修改不会自动写回旧文件。

## 自有表命名与结构

表名使用单数大写下划线，字段使用大写下划线并以 `_` 结束，如 `USER_ID_`。JSON、HTTP 接口和 Go / TypeScript 字段保持现有契约。SQLite 的 `sqlite_*`、FTS5 自动生成的影子表属于引擎内部对象，不作业务命名约束。完整建表快照见 [全局库](schema/global-management.sql)、[业务管理库](schema/source-management.sql)、[审计库](schema/audit.sql)。

全局 `management.db`：

| 表 | 主要字段与职责 |
| --- | --- |
| PLATFORM_USER | ID_, USERNAME_, NAME_, PASSWORD_HASH_, ACTIVE_, SYSTEM_ADMIN_, AUTH_VERSION_ |
| PLATFORM_PERMISSION | USER_ID_, PERMISSION_，平台系统权限 |
| SOURCE_REGISTRY | DATABASE_ID_, NAME_, MCP_PATH_, STATE_，只保存源目录 |
| AUTH_PROVIDER | ID_, NAME_, TYPE_, CONFIG_JSON_，认证提供方 |
| AUTH_POLICY | ID_, NAME_, MODE_, PROVIDER_ID_, CONFIG_JSON_ |
| AUTH_BINDING | TARGET_TYPE_, TARGET_ID_, POLICY_ID_，接口或数据库绑定 |
| AUTH_SETTING | ID_, REVISION_, DEFAULT_POLICY_ID_ |
| OAUTH_CLIENT | ID_, DATA_，持久化演示 OAuth 客户端注册 |
| SCHEMA_VERSION | VERSION_, APPLIED_AT_ |

每个业务 `management.db`：

| 表 | 主要字段与职责 |
| --- | --- |
| SOURCE_CONFIG | ID_, DATA_，数据库整体设置、结构快照及说明 |
| CONNECTION_CONFIG | PURPOSE_（READ / WRITE）, CONFIG_JSON_, PASSWORD_CIPHERTEXT_ |
| PLATFORM_GRANT | USER_ID_, DATABASE_ID_, PERMISSIONS_, BUSINESS_USER_ID_ |
| BUSINESS_ACCOUNT | ID_, DATABASE_ID_, USERNAME_, NAME_, BUSINESS_USER_ID_, PASSWORD_HASH_, PERMISSIONS_, ACTIVE_, AUTH_VERSION_ |
| QUERY_POLICY | TABLE_NAME_, SQL_, DATA_，SELECT 改写与字段 / 行权限 |
| QUERY_FRAGMENT | NAME_, SQL_，查询改写复用片段 |
| QUERY_TOOL | NAME_, DESCRIPTION_, SQL_, PARAMETER_JSON_，命名查询 |
| ACTION_DEFINITION | NAME_, TITLE_, DESCRIPTION_, CONFIRM_, PARAMETER_JSON_, SCRIPT_ |
| KNOWLEDGE | KEY_, DATA_，知识 / Skill |
| KNOWLEDGE_SEARCH | KEY_, NAME_, DESCRIPTION_, CONTENT_，由知识维护的 FTS 索引 |
| CONFIG_RECORD | BUCKET_, KEY_, DATA_，待确认结构扫描等辅助记录 |
| CONFIG_VERSION | BUCKET_, KEY_, REVISION_, DATA_, CREATED_AT_ |
| ACTION_RECEIPT | DATABASE_ID_, ACTOR_, REQUEST_ID_, FINGERPRINT_, REVISION_, STATE_, DATA_, TOKEN_, EXPIRES_ |
| SCHEMA_VERSION | VERSION_, APPLIED_AT_ |

连接池、编译后的改写 SQL、平台登录会话、OAuth 临时授权 / 刷新凭据、企业登录 state / PKCE 状态在内存中；磁盘管理库不保存 CONNECTION、SESSION、OAUTH_GRANT 等运行表。Action 回执与 OAuth 客户端注册有明确的跨重启需求，保留持久化。

## Auditor 记录什么

全局与逐库审计采用相同 `AUDIT_EVENT` 表：

```sql
ID_ TEXT PRIMARY KEY,
REQUEST_ID_ TEXT NOT NULL,
ACTOR_ID_ INTEGER NOT NULL,
DATABASE_ID_ TEXT NOT NULL,
KIND_ TEXT NOT NULL,
STARTED_AT_ TEXT NOT NULL,
DATA_ TEXT NOT NULL
```

前六列用于索引检索；`DATA_` 是完整事件 JSON：request_id、actor_id（平台用户）、subject_id（业务员工）、database_id、source、tool、policy_version、kind、status、original_sql、rewritten_sql、executed_sql、binds、error、started_at、finished_at、rows、truncated、detail。查询不保存结果集正文；Action 的 detail 包含参数、预览、写入计划和脚本返回值。密码、会话令牌和 OAuth 凭据不记录。

SQL 改写、执行、权限拒绝、超时、管理变更、认证及 Action 都会产生审计事件。事件进入容量 2048 的内存队列，后台独立写入；失败、队列满和关闭只记录警告 / 丢弃计数，不回滚或拒绝业务。进程异常退出可能丢失队列中尚未落盘的事件。审计与 Action 幂等一致性分开处理。

## 全局认证与身份来源

先配置策略大类，再选择提供方，再绑定入口：

| 大类 | 提供方 | 身份来源 |
| --- | --- | --- |
| ACCOUNT | PLATFORM_ACCOUNT | 全局平台用户，仅用于管理入口 |
| ACCOUNT | BUSINESS_ACCOUNT | 当前库 BUSINESS_ACCOUNT，适合测试库 |
| ACCOUNT | ENTERPRISE_ACCOUNT_API | 企业用户名 / 密码校验 HTTP 接口 |
| OAUTH | DEMO_OAUTH | DataScope 自建 OAuth，校验当前库业务账号 |
| OAUTH | ENTERPRISE_OAUTH | 企业授权 / 令牌 / UserInfo URL |
| NONE | 无 | 管理员配置的固定业务身份与能力 |

平台管理 API 必须保留明确的安全认证绑定，不允许匿名或使用演示业务账号。业务数据库可以绑定策略，也可跟随全局业务默认策略。管理 MCP 默认跟随平台策略，也支持 `API / management-mcp` 的企业 OAuth 绑定。企业身份不会被自动创建成 BUSINESS_ACCOUNT。

企业账号接口接收 `POST application/json`：

```json
{"username":"employee","password":"..."}
```

成功返回 HTTP 200 和可信声明，例如：

```json
{"sub":"stable-subject","username":"employee","employee_id":10,"name":"员工姓名","active":true}
```

`subject_claim` 默认 `sub`，用于稳定主体；`username_claim` 可指定账号名字段（用于匹配既有平台用户），不填则用主体；`business_id_claim` 默认 `employee_id`，业务查询映射为正整数员工 ID，与现有 SQL 权限变量一致。平台登录不要求 employee_id，但必须预先创建匹配账号并分配平台权限，不从企业响应自动授予系统管理员身份。企业业务能力由策略配置，忽略远端随意传入的 permissions。

企业 OAuth 配置 issuer、authorize_url、token_url、userinfo_url、client_id。浏览器使用 code + S256 PKCE、一次性 state 与 HttpOnly cookie；回调换取令牌后访问配置的 UserInfo。业务 / 管理 MCP 每次请求使用 Bearer token 在线访问 UserInfo 验证，不信任客户端自行传入的员工编号。企业需要提供兼容这些接口的适配服务；当前没有通用 SAML 或任意数据库表的认证查询适配器。

密钥只引用 `DATASCOPE_AUTH_*` 环境变量：账号接口使用 Bearer 服务端密钥，OAuth 令牌交换使用 client_secret。接口使用 HTTPS，本机测试允许 loopback HTTP。网络超时或认证失败会拒绝身份认证，不自动降级为匿名。

浏览器平台回调为 `{WEB_PUBLIC_URL}/api/auth/enterprise/callback`，Action 企业确认回调为 `{OAUTH_ISSUER}/api/auth/enterprise/callback`；按所用入口向企业注册。Web / API 应通过相同公开主机的反向代理暴露，浏览器会话 cookie 与回调保持同源。Vite 本地开发已代理 `/api` 和 `/actions`。

任何全局认证配置保存都会增加版本，使旧平台会话、演示业务令牌、待确认 Action 失效，需重新登录或预览。演示账号的密码、启用状态、能力和身份变更同样即时撤销旧令牌。

## 界面职责

- 数据库配置：基本信息与连接测试；业务认证直接展示。选择本库用户名密码或演示 OAuth 后，在认证设置下方直接显示“本地账号”卡片及完整账号列表；选择无需认证或企业提供方时隐藏。登录选项和单个账号编辑使用弹框，账号列表无需弹框。管理 / 审计路径收进基本信息的存储详情。
- 查询配置：直接展示表与字段权限，不保留页面级工具栏。重新载入、结构检查、变更记录、查询模板、SQL 片段和保存移入左侧“数据表”卡片标题区，以图标和悬停 / 聚焦提示呈现；全部开放 / 关闭合并为“开放”列头的总开关，支持部分开放状态；操作始终作用于全部已登记表，不受搜索过滤影响。表数量移到“数据表 / 名称”列头右侧，搜索时显示匹配数 / 总数。“重新载入配置”只读取已保存设置，“检查表结构”检查业务库的表、字段和注释。“查询模板”为可选弹框入口，将常用 SELECT 保存为独立 MCP 查询工具。

- 动作配置：左侧搜索与选择动作，右侧直接编辑代码和参数并保存；支持新增、复制、删除与始终回滚的试运行。切换动作时保护未保存修改。动作执行开关和网络数据库写入账号放入右侧编辑器标题栏，不单独占一行；没有动作时也保留开关。
- 动作工作台：工具区平铺入口 `#/action-workbench`，先调用 list_actions 获取列表，再选择动作调用 describe_action 查看参数 Schema，填写参数、业务身份和 request_id 后调用 try_action。分别展示说明与试运行的参数、JSON 和耗时，结果包含返回值和 SQL 写入计划，事务始终回滚。查看需要 action.write，试运行另需 impersonate 与已启用写入；不提供平台代替业务账号正式提交的通道。
- 知识管理：左侧选择知识，右侧直接编辑描述、关联表和 Markdown 正文，支持正文预览与草稿发布。列表项固定为标题和 ID 两行，发布 / 草稿 / 未保存状态放在右上角，长标题与 ID 省略显示并可悬停查看全文。
- 工具：独立 SQL 工作台与知识工作台、表数据查询、操作日志、MCP 开关 / 路径 / 工具开放范围。知识工作台模拟 search_knowledge → 选择 ID → read_knowledge 两步流程，各自展示参数、实际返回数据和耗时，正文另可预览；不会自动检索或预读正文。调用复用业务 MCP 的应用操作，按当前平台账号的 query 与 knowledge.read 权限过滤已发布知识；实际业务账号需单独登录业务 MCP 验证。

查询配置页的表和字段只展示最终生效的名称及注释，不展示来源对比或人工覆盖标签；点击“编辑”时才显示数据库原名称和原注释，并可恢复使用数据库注释。数据表列表、字段列表和主页面权限 SQL 编辑器按内容自然展开，共用页面外层纵向滚动；窄屏表格保留横向滚动。行权限区直接展开，过滤方式与行数限制横向排列；配置了前置检查时，检查 SQL 同样直接显示。方案分类、说明等通过“其他设置”维护。工具栏的“SQL 片段”打开复用片段编辑器；多个表的规则可以用 `{{片段名称}}` 引用同一段 SQL，片段本身不是可独立调用的查询工具。片段修改先应用到当前草稿，再与表权限一起保存。

全局认证入口在右上角系统设置中；查询模板和 Action 代码已从 MCP 配置移出。已有书签 `#/tables` 等继续有效。

左侧导航为单层平铺，用横线分隔管理入口、查询工具、日志和配置，不使用二级或折叠菜单；空权限分组不显示分隔线。

平台用户的数据库授权统一在“系统设置 → 用户管理 → 数据库权限”维护，按平台用户选择数据库和权限；业务库页面不提供平台用户授权入口。此处仅调整界面入口，授权记录仍按逐库管理存储。

## 逐库认证方式切换与 Token 登录

“数据库配置 → 业务认证”直接选择无需认证、账号密码（Token）、OAuth，并保存到当前库 `management.db` 的 `SOURCE_CONFIG.DATA_` 中的 `authentication.access`。无需新增表或字段；业务数据库结构不变。修改仅当前库生效，不修改其他库的策略，不使平台管理会话退出。未单独设置的旧库继续沿用原全局绑定 / 默认策略。

认证连接支持两种来源：复用系统设置中的全局提供方，或在当前库独立配置。账号密码可选择本库 `BUSINESS_ACCOUNT` 或企业账号接口；OAuth 可选择演示 OAuth 或企业 OAuth。无需认证时配置匿名业务身份及可用业务能力，查询仍执行表、字段和行权限。业务账号权限来自账号本身，企业身份权限来自当前库配置。业务认证设置仍由平台系统管理员修改。

账号密码登录接口：`POST <OAUTH_ISSUER>/auth/databases/<database_id>/token`，JSON 请求为 `{"username":"员工账号","password":"密码"}`，可选 `resource` 必须对应当前库的 MCP 地址。返回 `access_token`、`token_type: "Bearer"`、`expires_in: 3600`；后续 MCP 请求携带 `Authorization: Bearer <access_token>`。此接口独立于 OAuth 授权码流程；保留旧客户端的 HTTP Basic 兼容。Token 绑定数据库、认证配置和有效期，本库账号密码重置或停用立即使原 Token 失效；企业账号接口签发的 Token 在一小时内有效，修改本库认证设置可使其失效。Token 不落运行时实体表，不记录密码。

切换方式或登录选项后，业务客户端重新连接；来回切换不会恢复旧 Token。发布时继续携带各库完整 `management.db`、全局提供方配置及 `data/oauth/signing.pem`。

查询配置的权限编辑区与字段表格采用不同组件标识，反复切换数据表时保持只有一个“行权限与查询限制”区块。

## 全局提供方与本库独立认证连接

“数据库配置 → 业务认证”在账号密码 / OAuth 下提供两种配置来源：

- `GLOBAL`：按 `authentication.access.provider_id` 引用全局 `AUTH_PROVIDER`；旧配置省略 `source` 时按此方式兼容。
- `DATABASE`：在 `authentication.provider` 保存本库完整连接，并由 `authentication.access.provider_id` 明确引用其 ID。不读取或覆盖同名全局提供方；缺少提供方或验证失败直接拒绝，不回退到全局。

本库连接保存在当前库 `management.db` 已有 `SOURCE_CONFIG.DATA_`，无需新增表或字段。平台管理入口的认证不受本库编辑影响。界面支持本地业务账号、演示 OAuth、企业 OAuth 和企业账号 API；两种本地方式仍在下方直接显示完整账号列表。

企业 OAuth 配置 `issuer`、`authorize_url`、`token_url`、`userinfo_url`、`client_id`；企业账号 API 配置 `account_url`。`subject_claim` 默认 `sub`，`business_id_claim` 默认 `employee_id`，可额外指定 `username_claim`。密钥仍只使用 `DATASCOPE_AUTH_*` 服务端环境变量引用，不保存密钥明文；发布时配置对应环境变量。

MCP 的 `/.well-known/oauth-protected-resource/<MCP路径>` 在每次请求时解析当前库已保存的认证连接，OAuth 的 `authorization_servers` 返回该连接的 `issuer`。切换全局 / 本库连接保存后立即影响发现结果。企业服务自行提供其授权服务器发现文档，DataScope 不代理或伪造外部发现文档。界面中的“已生效的发现地址”指向当前 MCP 资源元数据；未保存草稿不影响发现结果。

独立企业身份按数据库及提供方划分主体标识，防止同名提供方或相同 `sub` 在不同库之间混用。修改本库连接使本库旧认证版本失效；已有来源不做自动切换。

## 知识 description 字段升级

知识编辑使用三行“描述”输入框，HTTP 和 MCP 的列表、搜索、读取与保存均以 `description` 为正式字段。列表 / 搜索完整返回描述（保留换行），不返回正文；每次最多 50 条。描述用于模型判断知识的用途和适用问题，正文仍按需读取。列表项仍保持标题与 ID 两行。

旧请求的 `summary` 作为兼容别名继续接收，同时提供两者时 `description` 优先，包括显式空字符串。旧记录也可读取；启动时将当前 `KNOWLEDGE.DATA_` 中的 `summary` 自动转成 `description`，保留知识 ID、发布状态、权限关联与 revision。历史 `CONFIG_VERSION` 快照不改写。

**管理库结构变化：** 每个业务管理库的 `KNOWLEDGE_SEARCH` 索引列 `SUMMARY_` 改为 `DESCRIPTION_`，相关插入 / 更新 / 删除触发器更新，来源库 `SCHEMA_VERSION` 记录版本 2。迁移以 SQLite 事务串行执行，并自动重建派生搜索索引；业务数据库表结构不变。发布携带升级后的各库 `management.db`，回退时使用升级前管理库快照。

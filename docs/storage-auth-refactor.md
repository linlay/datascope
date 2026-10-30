# 本地存储、认证与管理界面改造

本轮同时改造界面、管理存储与认证。业务库表结构与业务数据保持原状；DataScope 自有管理表发生了迁移，发布时必须携带新的管理目录。

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
| KNOWLEDGE_SEARCH | KEY_, NAME_, SUMMARY_, CONTENT_，由知识维护的 FTS 索引 |
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

- 数据库配置：整体连接信息与测试、显示名、管理 / 审计路径、认证来源、独立 Action 写连接、平台授权与演示业务账号。
- 查询配置：表映射与字段 / 行权限、SELECT 改写、复用片段、命名查询 SQL 与参数。
- 动作配置：独立 Action 列表、搜索分页、复制、结构化参数约束、JavaScript 高亮编辑、始终回滚的试运行及结果 / SQL。
- 知识管理：知识 / Skill 摘要、关联表、Markdown 正文编辑、草稿发布。
- 工具：SQL 工作台、表数据查询、操作日志、MCP 开关 / 路径 / 工具开放范围。

全局认证入口在右上角系统设置中；查询模板和 Action 代码已从 MCP 配置移出。已有书签 `#/tables` 等继续有效。

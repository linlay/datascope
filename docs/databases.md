# 多数据库接入与 Linux 部署

业务数据源可选择 SQLite、MySQL、PostgreSQL、Oracle、达梦 DM8、OceanBase（MySQL 模式）。全局管理、OAuth 客户端注册和独立审计继续使用 SQLite，逐库管理配置使用分目录 YAML。网络数据源通过 Go `database/sql` 驱动接入，没有引入 ORM。

## 平台与版本

| 数据源 | 驱动 | 本次选定的兼容目标 |
| --- | --- | --- |
| SQLite | modernc.org/sqlite | 原有 SQLite 文件 |
| MySQL | github.com/go-sql-driver/mysql v1.9.3 | MySQL 8.0+ |
| OceanBase（MySQL 模式） | 复用 github.com/go-sql-driver/mysql v1.9.3 | OceanBase 4.x MySQL 租户；目标版本须真实库验收 |
| PostgreSQL | github.com/jackc/pgx/v5 v5.7.6，stdlib 入口 | PostgreSQL 14+ |
| Oracle | github.com/sijms/go-ora/v2 v2.9.0 | Oracle 19c+，需要支持 OFFSET/FETCH |
| 达梦 | gitee.com/chunanyong/dm v1.8.23 | DM8；逐个实际版本、字符集和兼容模式验收 |

上述数据库版本为适配目标，真实连接测试通过后才能视为该环境已验收。编译成功不等于已经完成目标数据库的协议、权限或取消查询测试。

所有选定驱动可以在 `CGO_ENABLED=0` 下编译。macOS 开发时，DataScope 通过 TCP 连接远端 Linux 数据库；不要求在 Mac 上安装达梦或 Oracle 数据库服务。达梦 Go 驱动不同于 dmPython、ODBC 等客户端；本项目没有使用依赖平台原生客户端的接口。数据库服务端的系统与 CPU 支持范围由厂商对应发行版决定。

参考：[达梦 Go 编程指南](https://eco.dameng.com/document/dm/zh-cn/pm/go-rogramming-guide.html)、[达梦 Linux Go 连接示例](https://eco.dameng.com/document/dm/zh-cn/start/GO_DM_NEW.html)、[Go DM 模块来源](https://gitee.com/chunanyong/dm)。这里使用的是便于 Go Modules 引入的驱动分发仓库；生产部署前应核对目标 DM8 版本与厂商驱动要求。

## 连接配置

在管理界面点击“＋ 数据库”，填写显示名称、数据库类型、连接地址和账号密码，再测试连接、添加。地址与端口在同一行；切换类型自动填入 MySQL 3306、PostgreSQL 5432、Oracle 1521、达梦 5236、OceanBase 2881，也可改为自定义端口。数据库 ID 由服务端自动生成，改显示名称不会改变权限归属或连接器身份。新增数据源关闭 MCP，所有表默认禁止访问。配置访问策略、业务账号后再开放 MCP。

也可先选择数据库类型，再点击“从连接 URL 填写”展开输入框并粘贴 URL，浏览器会自动拆分连接字段，之后仍可逐项修改。URL 原文不提交到服务端；密码沿用原有加密凭据流程。支持 `mysql://`、`oceanbase://`、`postgres://` / `postgresql://`、`oracle://`、`dm://` / `dameng://`、`sqlite:`，以及对应的常见 JDBC 格式，例如：

```text
postgresql://reader:p%40ssword@db.example:5432/business?sslmode=verify-full&schema=public
jdbc:mysql://localhost:3306/business?user=reader&password=secret&useSSL=false
jdbc:oracle:thin:@//localhost:1521/FREEPDB1?user=BUSINESS&password=secret
jdbc:sqlite:data/existing.sqlite3
```

账号或密码中的 URL 特殊字符需要百分号编码。JDBC URL 未包含的账号、密码、schema 可在表单补充。Oracle 仅支持 service name；未支持的 URL 参数和 TLS 模式会明确报错。

SQLite 可选择“连接已有文件”或“新建项目数据库”。新建时只填写文件名（如 `business.sqlite3`），文件位于全局管理库所在数据目录的 `database/` 子目录，默认是 `data/database/`；该目录仍须在 `DATABASE_ALLOWED_ROOTS` 内。创建不会覆盖同名文件，登记失败会清理新文件。

系统管理员可在“数据库配置”中打开 **DDL 管理**（`ddl_enabled`，YAML 为 `ddlEnabled`），默认关闭，仅支持 SQLite。开启后当前数据库增加独立“DDL 管理”菜单；允许目录内已登记的本地业务文件也可开启，无需搬入受管目录。查看需要 `config.read`，修改另需 `ddl.write`；开关仅系统管理员可改。开关关闭时，Web 和 admin MCP 均拒绝 DDL 管理调用。管理库、审计库及其文件别名继续排除。

页面提供表、字段、约束、索引、数据库视图、触发器的查看、创建、修改、改名和删除，以及批量 DDL SQL、导出、变更预览、执行记录。原生不能直接完成的表定义修改通过重建表、同名字段数据迁移、关联对象恢复实现；字段改名使用独立 rename 操作。虚拟表创建、临时对象、CREATE TABLE AS SELECT、直接 DML、ATTACH、PRAGMA 和事务控制不开放。触发器定义可包含业务 DML。

DDL 预览在一致性副本上执行，校验数据约束、视图与触发器引用。计划绑定用户、数据库、配置版本及结构指纹，有效期 30 分钟。正式执行前再次校验，以单个事务执行整组 SQL，自动保存业务库一致性备份与管理配置/密钥快照，并记录执行 SQL 和前后差异；API 与 MCP 的 Action 写入按实际 SQLite 文件身份跨进程协调。重建表保留索引、触发器、视图、生成列及自增序列；结构定义与现有业务数据不兼容时拒绝，不自动清理或修正数据。执行最长 5 分钟。

服务端执行后触发结构侦查，在查询配置中审核应用；新表默认关闭，新字段不自动加入白名单。执行状态和备份持久保存在 `data/databases/<ID>/ddl/`（相对全局管理库目录），重启后可查；同一计划不会重复执行。结构成功但侦查失败时单独重试侦查，不能重放 DDL。备份恢复为独立运维操作，应停写、协调连接并核对备份之后的业务数据。普通查询连接仍保持只读，Action 仍仅运行 DML。

API 新建示例：`POST /api/databases`，参数 `{"name":"业务库","driver":"sqlite","path":"business.sqlite3","create_sqlite":true}`。DDL 使用 `/api/management/call` 的 `list_ddl_objects`、`get_ddl_object`、`preview_ddl`、`apply_ddl`、`get_ddl_execution`、`list_ddl_executions`；旧 `edit_sqlite_schema` 入口停止执行。admin MCP 的 `ddl.list/get/preview/apply/execution/history` 完整对齐，参数见 [DDL 管理参考](../deploy/connectors/datascope-admin/skills/datascope-admin/references/ddl.md)。

每个网络数据源通过 `connection.allowed_schemas` 登记允许范围，`connection.schema` 是可选默认 Schema。MySQL、OceanBase（MySQL 模式）、Oracle、PostgreSQL、达梦均支持同一连接、允许范围内的跨 Schema JOIN；不扩展到其他实例、连接、Oracle Service/PDB 或 DB Link，也不隐式登记视图和同义词。逻辑表名不区分大小写；同一个 Schema 中存在仅大小写不同的表时，登记会被拒绝。字段白名单使用结构读取返回的精确字段名。未加引号的字段和别名按目标库规则折叠大小写；带引号的字段和别名保留大小写。

MySQL/OceanBase 的 database 和 schema 表示同一个可选默认数据库，留空时两者均为空；PostgreSQL 的 database 仍必填，schema 可选；Oracle 的 service_name 仍必填（不使用 SID），schema 为可选默认 owner；达梦 schema 同样可选。默认 Schema 必须属于允许范围。

例：通过管理 API `POST /api/databases` 登记达梦（需要已登录的系统管理员）：

```json
{
  "name": "财务达梦库",
  "driver": "dameng",
  "password": "实际数据库密码",
  "connection": {
    "host": "10.0.0.20",
    "port": 5236,
    "schema": "FINANCE",
    "username": "DATASCOPE_READER"
  }
}
```

其他连接示例：

```json
{"driver":"mysql","connection":{"host":"10.0.0.21","port":3306,"database":"business","schema":"business","username":"datascope_reader","password_env":"DATASCOPE_DB_MYSQL_PASSWORD","tls_mode":"verify-full"}}
{"driver":"postgresql","connection":{"host":"db.example.internal","port":5432,"database":"business","schema":"public","username":"datascope_reader","password_env":"DATASCOPE_DB_POSTGRESQL_PASSWORD","tls_mode":"verify-full"}}
{"driver":"oracle","connection":{"host":"10.0.0.23","port":1521,"service_name":"FREEPDB1","schema":"BUSINESS","username":"DATASCOPE_READER","password_env":"DATASCOPE_DB_ORACLE_PASSWORD"}}
```

管理端默认直接输入数据库密码。测试连接只在内存中使用密码；接入成功后用 AES-256-GCM 加密，保存在当前库 `management/database.yml` 的 `READ_PASSWORD_CIPHERTEXT_` 或 `WRITE_PASSWORD_CIPHERTEXT_`，与连接设置一起发布。API/MCP 与审计只出现凭据引用，不返回明文或密文。两个服务读取相同的 YAML 与独立 `credentials.key`，文件权限为 0600。备份必须同时保存 YAML 和密钥；已有密文但密钥缺失时，禁止重新生成密钥。

仍兼容已有 `connection.password_env` 方式，例如 `.env` 中的 `DATASCOPE_DB_DAMENG_PASSWORD='实际密码'`，API 和 MCP 注入同名同值后使用；不能引用管理员密码等其他环境变量。直接密码放在创建请求顶层的 `password`，与 `connection.password_env` 二选一，不能把密码放入 connection。客户端可省略 id 和 port，服务端生成 ID 并补齐对应默认端口；已有手工 ID 继续兼容。

连接目标登记后固定，普通策略保存不会改变地址、schema、驱动或密码引用。更换目标或凭据引用时应新建数据源并重新配置权限，避免旧权限静默应用到另一套表。

MySQL、OceanBase、PostgreSQL 默认启用证书与主机名校验；可信开发网络可显式设置 `tls_mode: "disable"`。PostgreSQL 额外允许 `require`（加密但不验证身份）。当前 Oracle/达梦适配器只提供 TCP 连接，尚未接入钱包、厂商加密或客户端证书配置；需要这些能力的环境应先完成对应适配再部署。达梦驱动日志关闭，不保存 DSN。

未保存配置的连接测试使用管理动作 `test_database_connection`，参数为 `{database: {...}, password: "实际密码"}`（使用环境变量方式时不传 password），仅限系统管理员。管理 MCP 对应 `datascope_inspect` 的 `connection.test`。已有数据源继续使用 `database.test_connection`。

## OceanBase（MySQL 模式）

使用独立 `driver: "oceanbase"`，底层复用 MySQL 驱动、元数据查询与受限 SQL 方言；不增加客户端库或 CGO 依赖。当前仅支持 MySQL 兼容租户，不支持 OceanBase Oracle 模式。连接后读取 `@@ob_compatibility_mode`，不能确认 MySQL 模式时拒绝接入。兼容目标为 OceanBase 4.x，具体发行版、版本及 ODP 组合仍须真实库验收。

直连 OBServer 默认端口为 2881；通过 ODP 通常使用 2883，表单和 API 均可自定义端口。用户名按实际部署填写 `reader@tenant` 或 `reader@tenant#cluster`；云服务或代理提供专用连接账号时，以其连接信息为准。账号原样传给驱动，不自动拆分租户或集群。每个数据源固定一个连接目标及账号；允许 Schema 范围仅作用于当前租户内的数据库，不扩展到其他租户。

```json
{"name":"OceanBase 业务库","driver":"oceanbase","password":"实际数据库密码","connection":{"host":"obproxy.example.internal","port":2883,"database":"business","schema":"business","allowed_schemas":["business"],"username":"datascope_reader@tenant#cluster","tls_mode":"verify-full"}}
```

支持 `oceanbase://` 和 `jdbc:oceanbase://` 的单地址 URL，例如：

```text
oceanbase://reader%40tenant%23cluster:p%40ssword@obproxy.example.internal:2883/business
jdbc:oceanbase://obproxy.example.internal:2883/business?user=reader%40tenant%23cluster&password=p%40ssword
```

URL 中账号的 `@`、`#` 和密码特殊字符须百分号编码；表单账号字段直接填原文。database/schema 同义且必须一致，均可留空。默认 TLS 校验证书和主机名；只有明确使用可信开发网络时才选择关闭 TLS。沿用加密密码、独立 Action 写账号、默认拒绝、行列权限、脱敏及跨 Schema 限制；DDL 管理仍仅支持 SQLite。

真实库回归通过 `DATASCOPE_INTEGRATION_OCEANBASE` 提供 connection JSON，密码使用 `DATASCOPE_DB_OCEANBASE_PASSWORD`（或其他合法引用）：

```sh
export DATASCOPE_DB_OCEANBASE_PASSWORD='测试账号密码'
export DATASCOPE_INTEGRATION_OCEANBASE='{"host":"obproxy.example.internal","port":2883,"database":"business","schema":"business","username":"datascope_reader@tenant#cluster","password_env":"DATASCOPE_DB_OCEANBASE_PASSWORD","tls_mode":"verify-full"}'
make test-network
```

测试数据沿用下文 `datascope_records`，由管理员在隔离测试数据库预先准备。跨 Schema 回归使用 `DATASCOPE_SCHEMA_INTEGRATION_OCEANBASE`，格式同 [跨 Schema 验收](schema-join-acceptance.md)。单元测试和协议模拟不能替代真实库验收；上线前验证 ANSI_QUOTES/NO_BACKSLASH_ESCAPES、只读事务、TLS、注释/主外键、DECIMAL/日期/大字段、超时取消及 ODP 重连。Action 写入另需在隔离环境验证提交、回滚与失败处理。

参考：[官方 Go MySQL 驱动接入](https://en.oceanbase.com/docs/common-oceanbase-cloud-10000000001212290)、[连接账号及直连/ODP 端口](https://docs.oceanbase.com/obd/obd-takeover)、[兼容模式变量](https://docs.oceanbase.com/oceanbase-database/V4.4.2-system-management-set-variables)。

## SQL 与权限改写范围

查询工作台的 SQL 示例保存在各数据库自己的 `data/databases/<数据库 ID>/management/sql-examples.yml`，不使用全局示例。文件内容：

```yaml
sqlExamples:
  - label: 客户数量
    sql: SELECT COUNT(*) FROM customers
```

选择示例只填入编辑区，不自动执行；`sqlExamples: []` 表示不提供示例。YAML 使用 `sqlExamples`，管理 API 仍使用 `sql_examples`，修改需要 `config.write` 权限。`database.yml` 不再保存 SQL 示例。旧版内嵌示例可继续读取，下一次配置保存会原子拆分；升级已有配置可在停止 API/MCP 后运行 `make migrate-state`，迁移会备份原文件并保留 revision。

证券库的恢复模板同样位于该库的管理目录结构中：`examples/securities/sql-examples.yml`，仅供显式安装，不作为运行时回退。新库可通过 `import-config -file examples/securities/bundle.yml` 一次性导入，已有库通过管理控制台维护；导入器不会覆盖已有自定义配置。

数据库目录使用内部 ID。证券演示库 ID 为 `securities_demo` 时，示例路径为 `data/databases/securities_demo/management/sql-examples.yml`；安装命令默认读取 `MCP_DATABASE`，也可用 `-database securities_demo` 显式指定。

查询工作台的规范路由为 `/#/query-workbench?database=<数据库 ID>`，旧 `/#/sql` 和 `/#/sql-workbench` 链接会保留资源选择和参数并替换为新路由。

网络数据源第一版接收受限 SELECT 方言。解析器仍以 SQLite 的语法树为内部表示，再由 `internal/dialect` 校验并生成目标 SQL；这不是各数据库全部原生语法的支持声明。数据库报错时不会回退到原 SQL。

- 支持单条 SELECT、WHERE、显式 JOIN ON、子查询、非递归 CTE、GROUP BY/HAVING、ORDER BY、UNION，以及非负整数 LIMIT/OFFSET。部分集合或 JOIN 形式按库拒绝。
- 支持 COUNT、SUM、AVG、MIN、MAX、ABS、ROUND、LOWER、UPPER、LENGTH、COALESCE、NULLIF、SUBSTR。内置函数的类型与空值语义仍以目标数据库为准。
- 暂不支持递归 CTE、窗口函数、用户函数、表值函数、CAST、专有运算符、索引提示、MATERIALIZED、存储过程、写入型 CTE、SELECT INTO 和 FOR UPDATE。
- Oracle 自动移除表别名的 AS，常量查询从 SYS.DUAL 读取，LIMIT/OFFSET 生成 OFFSET/FETCH。
- MySQL/OceanBase 每个连接使用 ANSI_QUOTES 和 NO_BACKSLASH_ESCAPES，使字符串规则与内部解析一致。
- PostgreSQL 固定 search_path 为 pg_catalog，业务表始终显式限定 schema。

网络权限 SQL 使用完整物理表名，例如：

```sql
SELECT * FROM sales.orders WHERE owner_id = ${userId}
```

内部生成的 `main."sales.orders"` 在执行前映射到完整物理表名。旧配置升级把原 `main.orders` 固定为原默认 Schema 下的 orders，避免更改默认 Schema 后策略转向另一张表。客户端可以使用完整表名与显式表别名；未限定表名仅解析到显式默认 Schema，没有默认 Schema 时拒绝未限定表名。`${userId}` 和 `${asOf}` 只能由服务端提供；业务工具参数仍使用 `:arg_参数名`。生成 SQL 按出现顺序绑定参数，包括同一参数多次出现的情况。网络库的 `${asOf}` 使用带类型的时间绑定，SQLite 保持原来的字符串格式。

查询预览、策略保存、前置检查和业务工具验证都经过方言校验。`raw_query` 不进行业务权限改写，但同样只接受这一只读 SELECT 子集。演示库的 customer_scope 和员工目录校验仅适用于 SQLite 证券演示库；网络数据源使用平台托管的逐库业务账号。

候选发现只读取 Schema 与表名，不读取全量字段、注释或业务数据；浏览页的匹配总数通过 COUNT 查询。结构中的 DDL 是元数据摘要，不是完整的建表脚本。达梦 LOB 转换时每字段限制为 1,048,576 字符（CLOB）或字节（BLOB），超限明确报错。

## 只读运行与 Linux 构建

数据库账号只授予登记 schema 中需要的表的 SELECT 权限，不使用 SYSDBA、数据库超级用户或业务写入账号。查询执行还建立只读事务：MySQL/OceanBase/PG 使用只读事务选项；Oracle/达梦显式执行并检查 SET TRANSACTION READ ONLY，避免驱动不支持或忽略只读设置错误。无法建立只读事务时拒绝登记和查询。

服务端设置连接、读取、语句执行超时和返回行数上限；不同驱动的服务端取消效果仍应使用真实库验收。行数上限仅限制返回结果，不等同于限制数据库扫描或聚合计算量。

```sh
# macOS 开发环境交叉构建 Linux x86_64
make build-linux
```

当前 Linux 发布目标为 x86_64（amd64）。输出位于 `bin/linux-amd64/`，包含 admin-api 与 mcp-server；前端位于 `web/dist/`。两个进程继续共享同机的管理库和审计库，按照原部署方式配置前端与反向代理。

仅使用网络数据库时，在两个进程设置 `SQLITE_PATH=`，或传 `-db ''`。首次启动可保持空数据源列表，再从控制台登记。后续启动不要求存在 default 数据源或证券演示文件。首次初始化仍须配置 ADMIN_PASSWORD；示例账号只在显式导入时读取 DATASCOPE_EXAMPLE_PASSWORD。

## 真实数据库验收

单元测试覆盖参数、SQL 边界、拒绝规则和权限改写语义；真实库测试单独启用，不会自动修改数据库或建立对象。

在专用测试 schema 使用管理员预先创建以下三行数据，再给 DataScope 只读账号 SELECT 权限。MySQL、OceanBase MySQL 模式、PG、Oracle、DM 均可使用以下基础 DDL（在目标 schema 中执行，Oracle/DM 常规未引用标识符会存为大写）：

```sql
CREATE TABLE datascope_records (
  id INTEGER PRIMARY KEY,
  owner_id INTEGER NOT NULL,
  secret VARCHAR(100)
);
INSERT INTO datascope_records VALUES (1, 1, 'first');
INSERT INTO datascope_records VALUES (2, 2, 'second');
INSERT INTO datascope_records VALUES (3, 1, 'third');
COMMIT;
```

设置连接 JSON，密码仍用独立环境变量：

```sh
export DATASCOPE_DB_DAMENG_PASSWORD='测试账号密码'
export DATASCOPE_INTEGRATION_DAMENG='{"host":"10.0.0.20","port":5236,"schema":"TEST_SCOPE","username":"DATASCOPE_READER","password_env":"DATASCOPE_DB_DAMENG_PASSWORD"}'
make test-network
```

其他变量为 DATASCOPE_INTEGRATION_MYSQL、DATASCOPE_INTEGRATION_OCEANBASE、DATASCOPE_INTEGRATION_POSTGRESQL、DATASCOPE_INTEGRATION_ORACLE，内容为对应 connection 对象。未配置的数据库显示 SKIP，不能计为验收通过。测试覆盖元数据、默认拒绝、行与字段权限、JOIN/CTE、前置拒绝、分页与写入拒绝。上线前额外验证目标版本的 TLS、字符集、日期/DECIMAL/LOB、连接中断以及服务端超时取消。

## Schema 范围与选表

数据库管理登记允许的 Schema，查询管理通过“选择登记表”搜索并分页选表。登记时校验实际 SELECT 能力，读取所选表的字段、注释并建立结构快照；新登记表默认关闭，随后通过现有表开放开关和 MCP 发布流程授权。未选表不建立字段快照。移除 Schema 会移除其已登记结构和策略，再添加时须重新登记、开放，不继承其他 Schema 同名表权限。

网络表的 name 是 `Schema.TABLE_NAME`，另有 schema 和 table_name 元数据字段。策略、注释、脱敏域和查询物理表名均保留 Schema。表逻辑标识仍不区分大小写，冲突时拒绝登记；名称含点的 Schema/表暂不纳入候选，避免歧义。SQLite 使用固定 main 上下文，维持原有裸表名和单库行为，不开放 ATTACH。

省略 allowed_schemas：存在默认 Schema 时只登记默认 Schema；没有默认 Schema 时在创建时发现可见 Schema 并冻结为显式列表。显式空列表表示不允许任何 Schema。以后出现的新 Schema 不自动加入。元数据候选仍须在选表时验证读取权限；数据库撤销授权后，源数据库会拒绝后续读取。

自动结构检查对网络源只检查已登记表；候选表增多不会自动扩展快照。SQLite 保留原有完整结构发现流程。连接测试返回 connected、empty、table_count 和 schemas，零表仍属于连接成功。

普通查询在执行前检查每张表和所有表达式中的字段，包括 JOIN/WHERE、排序、子查询、CTE 和 UNION 分支；每张表保留原行权限改写。raw_query 是单独授权的高权限测试入口，绕过表开放、行权限和字段白名单，仍受只读方言、Schema 范围、脱敏、超时与结果上限限制。

已有网络配置启动时通过管理存储原子迁移至 schema_identity_version=1，保留表/字段权限、注释和脱敏；旧单库/单 Schema 的允许范围固定为原默认值。跨 Schema 测试数据与真实数据库验收方法见 [跨 Schema 验收](schema-join-acceptance.md)。

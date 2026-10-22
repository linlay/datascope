# SQL Rewrite 数据库代理管理平台

支持管理多个 SQLite 数据源，逐库配置 MCP 接口、表和字段访问、行权限 SQL 改写及查询返回行数上限。

三个服务：MCP server（默认 8080）、admin-api（8081，含 OAuth 授权服务器）、React 控制台（8082）。Go 数据生成器生成合成证券业务库；AI 提交的 SELECT 经 AST 改写后，只能查询当前员工可见的数据。金额和价格均为**整数分**，股数为整数股。

## 启动

需要 Go 1.26、Node 22+。

```sh
cp .env.example .env
# 设置 .env 中 OAUTH_DEMO_PASSWORD（至少 12 字符）
make init          # 固定种子生成；已有目标文件会拒绝覆盖
make web-install
make build
make run-api       # 终端一，先启动以发布 JWKS
make run-mcp       # 终端二
make run-web       # 终端三
make test          # 独立临时库，Go 测试 + 前端类型检查
```

本工作区 `.env` 保留现有端口（MCP 8083），已切换到新库并配置随机演示密码。登录工号如 `E0001`（客户经理）、`E0121`（营业部主管）、`E0151`（分公司主管）、`E0161` / `E0162`（两类总部分管）、`E0175`（管理员）。密码读取本机 `.env` 的 `OAUTH_DEMO_PASSWORD`，不进入 Git。该配置值是明文密码，即使看起来像随机字符串也直接原样输入，无需解密。`E0176`–`E0178` 为离职员工，不能登录。

二进制启动也需加载 `.env`：`set -a; . ./.env; set +a; ./bin/admin-api`。`make run-*` 会自动加载。

## 多数据库管理

左上角按数据库名称切换，或添加服务端已有的 SQLite 文件。首次导入时使用数据库文件名（不含扩展名）作为显示名称，例如 `securities_demo`；可在管理页面修改名称。旧配置中的“默认数据库”会自动迁移为对应文件名。当前支持多个 **SQLite 数据库实例**，尚未接入 MySQL / PostgreSQL 驱动及方言。普通 SQLite 文件不需要演示库的 `schema_comments` 表。

1. 添加数据库：填写名称、唯一 ID 与文件路径。连接只读；新数据源默认关闭 MCP、禁止所有表。
2. 在“数据库访问策略”选择表，设置是否开放、字段白名单、行权限 SQL 模板、前置 `deny_unless` 检查和最多返回行数。`columns: null` 允许全部字段，包括未来新增字段；非空数组只开放指定字段。
3. 行权限模板继续支持 `main.表名`、`${userId}`、`${asOf}` 和当前数据库的 `{{片段}}`。字段白名单应用在行权限子查询外层，因此 `SELECT *` 仅返回白名单字段，隐藏字段也不能用于外层 WHERE、JOIN 或子查询。
4. 在“MCP 配置与测试”中勾选 MCP 接口并打开 MCP，点击“保存并生效”。每库可独立启停 `list_tables`、`describe_table`、`query`、`raw_query`、`customer_scope`；保存后，HTTP / stdio 后续请求立即采用当前配置。已经执行中的请求使用请求开始时的配置。
5. “表数据”“SQL 控制台”“数据库访问策略”和“MCP 配置与测试”跟随当前数据库；切换数据库时保留所在菜单。表数据与原始 SQL 是管理员全量视图；“改写执行”和 MCP 查询执行访问策略。

最大返回行数取请求值、涉及各表上限与全局硬上限（1000）中的最小值；请求未指定时默认 200。此项限制**结果行数**，不限制聚合查询扫描的底层记录数；执行时间仍受 `QUERY_TIMEOUT` 限制。`max_rows: 0` 表示使用全局限制。

两个 Go 进程必须使用同一个 `DATABASE_CONFIG_DIR` 目录（默认 `config/databases`，本地配置已加入 Git 忽略）。**每个数据库一个独立 `.yml` 文件**，同时保存名称、连接信息、逐表访问策略、SQL 片段、MCP 开关和接口列表：

```text
config/databases/
  securities_demo.yml   # 原证券演示库，兼容内部 ID default
  shop_demo.yml         # 小型商店测试库，ID shop_demo
```

初始库文件名来自 SQLite 文件名，其他库文件名使用数据库 ID。首次升级会从旧的 `DATABASE_CATALOG`（默认 `data/databases.json`）迁移；已有 YAML 时不再读取旧 JSON，也不会用初始 `config/rewrite.yml` 覆盖已保存策略。旧 JSON 可另存为 `.bak` 备份。

管理页面保存时只写对应数据库的 YAML。支持直接编辑 YAML，后续请求读取新配置；修改后刷新管理页即可继续编辑。文件锁、原子替换、版本和内容指纹检查用于避免并发或手工修改被旧页面覆盖。字段 `columns: null` 表示全部字段，`columns: []` 表示未选择字段（开放表不允许为空）。未知字段、重复数据库 ID 或错误 YAML 会报错。备份整个配置目录即可保留所有数据库配置。数据库新增或删除表后需同步调整该库 YAML 中的逐表策略；规则与实际表不匹配时查询拒绝执行。

| 用途 | 地址 |
| --- | --- |
| 数据库目录 | `GET /api/databases`、`POST /api/databases` |
| 查看 / 保存数据库策略 | `GET /api/databases/{id}`、`PUT /api/databases/{id}` |
| 当前数据库管理 API | `/api/databases/{id}/tables`、`/query`、`/rewrite`、`/mcp/info`、`/mcp/call` |
| 分库 MCP | `/mcp/{mcp_path}`（逐库配置后缀，`MCP_PATH` 调整公共前缀） |
| 兼容默认库 | `/mcp`、原有 `/api/tables` 等接口 |

每个数据库可在“MCP 配置与测试”中单独设置 MCP 路径后缀，例如 `mcp_path: securities` 对应 `/mcp/securities`。后缀以字母或数字开头，仅支持字母、数字、下划线、连字符，最长 64 位，所有数据库间必须唯一（包含关闭 MCP 的数据库）。修改后立即生效，旧的分库路径返回 404，页面测试自动采用新地址；旧客户端需要更新连接地址。未配置 `mcp_path` 的旧文件兼容使用数据库 ID，启动时自动补齐字段。`/mcp` 仍作为初始库的兼容入口，stdio 仍按数据库 ID 选择，不依赖 HTTP 路径。自定义路径使用该完整端点地址作为独立 OAuth 资源。

HTTP 固定使用无状态 MCP 请求，以便每次请求读取最新策略。stdio 通过 `-database <id>` 或 `MCP_DATABASE` 选择数据库，也遵循该库服务与接口开关。`raw_query` / `customer_scope` 可按库选择开启，默认不启用。`raw_query` 绕过表、字段及行权限改写，数据库仍保持只读；两个工具仅管理员可调用，OAuth 下另需 `sql:debug`。`customer_scope` 适用于证券样例库。

OAuth 共用员工身份体系与授权服务器，但每个数据库连接独立授权。`MCP_RESOURCE_URL` 配置公共资源根地址，各库完整 MCP 地址分别作为资源元数据的 `resource`、授权请求的 `resource` 和 JWT 的 `aud`；例如 `/mcp/securities` 的令牌不能用于 `/mcp/shop_demo`。授权码和刷新令牌也绑定该完整地址。修改 MCP 路径后客户端需要更新地址并重新授权，关闭 MCP 的数据库不能新建或刷新授权。所有新增数据源的 `${userId}` 都使用这套可信身份编号。

## 小型商店测试库

执行 `make init-shop`（或 `go run ./cmd/gen-shop`）创建 `data/shop_demo.sqlite3`；已有文件会拒绝覆盖。结构和合成数据见 `sql/shop_demo.sqlite.sql`：4 位客户、6 件商品、8 笔订单，共三张表 `customers`、`products`、`orders`。金额使用整数分，客户归属员工编号为 1、2，商品包含用于字段隔离测试的 `cost_cents`。

在管理平台添加该文件、数据库 ID 填 `shop_demo`、名称填 `shop_demo`，即生成 `config/databases/shop_demo.yml`。新增库沿用默认关闭 MCP、禁止所有表的初始策略；可分别在“数据库访问策略”和“MCP 配置与测试”中配置。

## 数据与生成器

```sh
go run ./cmd/gen-data -db data/another.sqlite3 -seed 20261021 -customers 1500
```

生成器不会覆盖已有文件；失败会移除不完整输出。旧演示库在本工作区保留为 `data/legacy/finance_demo.sqlite3`，旧 Python 与 MySQL 生成脚本已移除，可从 Git 历史恢复。新库、私钥与 OAuth 状态不提交 Git；无需依赖二进制数据库运行测试。

17 张表：岗位、分公司、营业部、员工、管理范围、客户、资金账户、证券账户、证券目录、交易日历、行情、委托、成交、流水、日终持仓、资金日结及数据字典。默认 100 名员工、1,500 位客户、30 个营业部、10 家分公司和 60 只模拟 A 股证券。时间为 2026-05-06 至 2026-10-20，共 113 个交易日，保留全部日终快照；数据规模随生成参数和账户分布变化。

基础表直接使用业务主键：`branches.code`、`departments.code`、`securities.code`、`customers.customer_no`、`fund_accounts.fund_account_no` 和 `employees.employee_id`。证券代码和账号使用字符串保存，保留前导零；委托、成交及流水等业务记录继续保留整数主键。员工编号保留常用演示身份，允许不连续，不使用自增生成。

客户包含个人与企业（`customer_type`）。默认 95% 为个人，少部分个人有 2–3 个资金账户；5% 为企业，每家有 4–10 个资金账户。客户表只保存客户资料；营业部及客户经理归属位于资金账户，分公司由营业部关联。一个客户的账户可以分属不同营业部及客户经理。每个资金账户分别关联沪、深交易所股东号（`stock_accounts.fund_account_no`），委托中的股东号必须属于该资金账户并匹配证券交易所。交易、流水、持仓与日结按资金账号独立核算，关联字段统一使用 `customer_no`、`fund_account_no` 和 `security_code`。证券名称使用科技、实业、发展、银行、证券、期货、保险等行业风格的虚构名称。

券商“星澜证券”和证券名称均为虚构。身份证和手机号在生成时脱敏；不生成完整证件号或真实联系号码。证券代码仅用于演示沪深及科创板格式，行情与交易均非市场真实记录。

买入受资金约束；卖出受开盘可用持仓约束（T+1）。普通股票委托 100 股整数倍，科创板买入至少 200 股。成交包含拆单；佣金每委托万分之 2.5、最低 5 元（分摊到成交），卖出印花税 0.05%，双向过户费 0.001%，均为演示费率。日结包含转入、转出、买卖与费用。内置一致性校验检查外键、成交数量、持仓滚动、资金滚动、日结与成交汇总，以及非负余额。

交易日历依据 [上交所 2026 年休市安排](https://www.sse.com.cn/disclosure/dealinstruc/closed/)，周末不因调休开市；端午 6 月 19 日、中秋 9 月 25 日、国庆 10 月 1–7 日休市。

结构见 [DDL](sql/schema.sqlite.sql)，字段与规模见 [数据字典](docs/data-dictionary.md)，权限定义见 [权限说明](docs/permissions.md)。

## 权限改写

`config/rewrite.yml` 是默认库的初始 SQL 规则来源；管理平台启用后，由共享数据库目录保存逐库规则。每张表必须显式配置，缺失或多余表、错误模板、循环片段、未知变量均导致启动失败。`${userId}` 绑定内部员工 ID，`${asOf}` 绑定权限参考日期；SQL 文本不拼接身份值。OAuth JWT 的 `sub` 是工号，由 MCP 校验后映射为内部 ID。

| 方案 | 表 | 行为 |
| --- | --- | --- |
| NONE | roles、branches、departments、securities、trading_calendar、daily_quotes、schema_comments | 公共字典 |
| CUSTOMER | customers、fund_accounts、stock_accounts、orders、trades、cash_flows、positions、daily_balances | 在职管理员看全部；其余在职员工按资金账户归属及有效机构范围查询，客户资料由可见账户确定 |
| EMPLOYEE | employees | 在职员工看本人及范围内员工；管理员看全部 |
| RESTRICTED | employee_scopes | 仅在职管理员允许查询，其他身份整条拒绝 |

范围由 `employee_scopes` 决定，角色本身不给主管增加范围。按客户**开户营业部**判断主管可见性，跨营业部服务客户仍可由其客户经理查询。生效日含当天，失效日不含当天；离职或未知身份没有客户与员工数据。公共字典仍可查询。

SELECT 支持 JOIN、嵌套查询、CTE、UNION、窗口函数；拒绝写操作、多语句、未知表、schema 限定、同名 CTE、绑定参数、表值函数及 `IN 表名`。SQLite 连接以 `mode=ro` 和 `query_only` 打开。前端可编辑逐表 SQL 模板与共享片段，并在工作台查看改写 SQL、绑定变量和本次绑定值。

## OAuth 与 MCP 接入

实现按 [MCP Authorization 规范](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization) 分为授权服务器和资源服务器，使用官方 Go SDK 的 `auth.RequireBearerToken` 与受保护资源元数据处理器。

1. 在 agent-platform 使用“MCP 地址导入”，地址使用管理平台显示的所选数据库完整 MCP 地址（例如 `http://127.0.0.1:8083/mcp/default`），保持 `authMode: mcp`。
2. “账号授权”会通过资源元数据发现 `OAUTH_ISSUER`，动态注册公共客户端，再打开员工登录授权页。
3. 输入工号与配置的演示密码，同意查询授权。客户端以授权码 + PKCE S256 换取令牌，必须携带精确 `resource`。
4. 换身份时重新发起账号授权登录另一员工。本服务不建立自动登录会话。

端点：`/.well-known/oauth-authorization-server`、`/oauth/register`、`/oauth/authorize`、`/oauth/token`、`/oauth/jwks.json`；MCP 端为各连接提供 `/.well-known/oauth-protected-resource/mcp/{mcp_path}`，资源标识与该连接完整地址一致；另保留 `/mcp` 对应的元数据与根路径别名。未携带或无效令牌返回 401 和 `WWW-Authenticate` 发现地址。令牌校验签名、issuer、audience、过期时间、scope 与当前员工状态，忽略 `X-User-Id`。

JWT 使用 RS256，有效 1 小时，`sub=工号`、`aud=当前连接的完整 MCP 地址`（兼容 `/mcp` 入口时为公共根地址）。刷新令牌有效 7 天，单次使用并轮换；重用旧刷新令牌会撤销该刷新族。授权码 2 分钟、单次使用，绑定客户端、redirect URI、resource 与 PKCE；登录表单有 CSRF 校验。私钥仅由 admin-api 持有（0600 文件），MCP 通过固定 issuer 拉取 JWKS，缓存 5 分钟。客户端注册与授权状态保存到独立 SQLite，重启后可继续使用。

普通员工只签发 `sql:query`；管理员请求时可获得 `sql:debug`。管理模式提供五个工具供逐库勾选，默认只启用三个基础工具；`MCP_DEBUG` 控制 query 是否返回改写 SQL 和 binds，工具启停由数据库 YAML 的 `tools` 列表决定。SQL 工作台也可查看改写结果和 binds。

`MCP_AUTH_MODE`：

- `oauth`：默认 HTTP 模式，配置 issuer、resource 与演示密码。外部连接必须把这两个地址设为客户端实际可达的 HTTPS 地址（本机允许 loopback HTTP）。
- `header`：仅用于可信网关，`X-User-Id` 是内部员工 ID；网关必须剥离外部同名头。缺失时可使用显式默认员工。
- `none`：本地调试，忽略身份头，仅用 `MCP_DEFAULT_USER_ID`。stdio 必须使用此模式。

`OAUTH_DEBUG_LOGIN=true` 会在授权页出现无需密码的员工模拟选择器，仅本机演示使用，默认关闭。后台“MCP 配置与测试”由 admin-api 为所选在职员工签发令牌，真实通过 MCP 校验与改写，不由前端持有签名私钥。离职员工不能签发令牌，可在 SQL 工作台验证其空范围。

客户端应按完整 MCP 地址分别保存各数据库连接的授权凭据；即使客户端在部署内共享凭据存储，也不能将一个数据库的令牌用于另一个数据库。本仓库集成测试覆盖分库元数据严格校验、登录、PKCE 换令牌、刷新与 MCP 调用；外部 Platform 的浏览器回调仍需实际联调。

## 控制台与部署边界

控制台提供数据库切换与新增、全量数据浏览、SQL 工作台、逐表访问策略编辑和 MCP 调试；它与后台 API 是本机管理员调试工具，**没有后台登录认证**，不要直接暴露到公网。部署时通过反向代理仅公开 `/oauth/*`、`/.well-known/*` 与 MCP 端点，将 `/api/*`、开发前端限制在本机或受保护管理网络。

统一演示密码用于本次演示，正式部署应接入员工密码哈希或企业身份系统。OAuth 注册提供 `token_endpoint_auth_method=none` 的公共客户端；不支持客户端密钥、OneID 或客户本人登录。更换签名私钥会使已签发 JWT 失效（JWKS 缓存最长 5 分钟）；OAuth 状态库与私钥必须妥善保留。

## 验证

`make test` 为每个测试进程生成独立临时库，覆盖：全员权限与独立 Go 参考算法、四个时间边界、跨机构客户、员工可见性、数据一致性、SQL 拒绝规则、HTTP MCP 协议、OAuth 登录 / PKCE / CSRF / 授权码重放 / 刷新轮换及重用 / 无效 JWT / 管理员调试权限，多数据库持久化与隔离、字段白名单与隐藏字段过滤、每表行数上限、HTTP / 长连接 MCP 接口实时撤销，以及前端 TypeScript 类型检查。

## 登录故障排查

密码不匹配时返回“工号或密码错误”。授权页过期、重复提交或浏览器未保存校验 Cookie 时会显示重新发起授权的提示。重复打开的授权页分别持有独立校验 Cookie，互不覆盖。升级后若仍停留在旧错误页，请回到 agent-platform 再次点击“账号授权”，在新页面输入原工号和明文密码。不要重复提交已经使用过的旧表单。

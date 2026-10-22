# 证券经纪业务 SQL 权限改写演示

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

`config/rewrite.yml` 是唯一 SQL 规则来源，两个 Go 服务读取同一份。每张表必须显式配置，缺失或多余表、错误模板、循环片段、未知变量均导致启动失败。`${userId}` 绑定内部员工 ID，`${asOf}` 绑定权限参考日期；SQL 文本不拼接身份值。OAuth JWT 的 `sub` 是工号，由 MCP 校验后映射为内部 ID。

| 方案 | 表 | 行为 |
| --- | --- | --- |
| NONE | roles、branches、departments、securities、trading_calendar、daily_quotes、schema_comments | 公共字典 |
| CUSTOMER | customers、fund_accounts、stock_accounts、orders、trades、cash_flows、positions、daily_balances | 在职管理员看全部；其余在职员工按资金账户归属及有效机构范围查询，客户资料由可见账户确定 |
| EMPLOYEE | employees | 在职员工看本人及范围内员工；管理员看全部 |
| RESTRICTED | employee_scopes | 仅在职管理员允许查询，其他身份整条拒绝 |

范围由 `employee_scopes` 决定，角色本身不给主管增加范围。按客户**开户营业部**判断主管可见性，跨营业部服务客户仍可由其客户经理查询。生效日含当天，失效日不含当天；离职或未知身份没有客户与员工数据。公共字典仍可查询。

SELECT 支持 JOIN、嵌套查询、CTE、UNION、窗口函数；拒绝写操作、多语句、未知表、schema 限定、同名 CTE、绑定参数、表值函数及 `IN 表名`。SQLite 连接以 `mode=ro` 和 `query_only` 打开。前端显示 YAML、展开 SQL、绑定变量和本次绑定值。

## OAuth 与 MCP 接入

实现按 [MCP Authorization 规范](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization) 分为授权服务器和资源服务器，使用官方 Go SDK 的 `auth.RequireBearerToken` 与受保护资源元数据处理器。

1. 在 agent-platform 使用“MCP 地址导入”，地址使用 `MCP_RESOURCE_URL`，保持 `authMode: mcp`。
2. “账号授权”会通过资源元数据发现 `OAUTH_ISSUER`，动态注册公共客户端，再打开员工登录授权页。
3. 输入工号与配置的演示密码，同意查询授权。客户端以授权码 + PKCE S256 换取令牌，必须携带精确 `resource`。
4. 换身份时重新发起账号授权登录另一员工。本服务不建立自动登录会话。

端点：`/.well-known/oauth-authorization-server`、`/oauth/register`、`/oauth/authorize`、`/oauth/token`、`/oauth/jwks.json`；MCP 端提供 `/.well-known/oauth-protected-resource/mcp` 及根路径别名。未携带或无效令牌返回 401 和 `WWW-Authenticate` 发现地址。令牌校验签名、issuer、audience、过期时间、scope 与当前员工状态，忽略 `X-User-Id`。

JWT 使用 RS256，有效 1 小时，`sub=工号`、`aud=MCP_RESOURCE_URL`。刷新令牌有效 7 天，单次使用并轮换；重用旧刷新令牌会撤销该刷新族。授权码 2 分钟、单次使用，绑定客户端、redirect URI、resource 与 PKCE；登录表单有 CSRF 校验。私钥仅由 admin-api 持有（0600 文件），MCP 通过固定 issuer 拉取 JWKS，缓存 5 分钟。客户端注册与授权状态保存到独立 SQLite，重启后可继续使用。

普通员工只签发 `sql:query`；管理员请求时可获得 `sql:debug`。`MCP_DEBUG=true` 才注册 `raw_query` / `customer_scope`，这两个工具另需管理员身份，OAuth 下还必须有 `sql:debug`。调试查询返回改写 SQL 和 binds。

`MCP_AUTH_MODE`：

- `oauth`：默认 HTTP 模式，配置 issuer、resource 与演示密码。外部连接必须把这两个地址设为客户端实际可达的 HTTPS 地址（本机允许 loopback HTTP）。
- `header`：仅用于可信网关，`X-User-Id` 是内部员工 ID；网关必须剥离外部同名头。缺失时可使用显式默认员工。
- `none`：本地调试，忽略身份头，仅用 `MCP_DEFAULT_USER_ID`。stdio 必须使用此模式。

`OAUTH_DEBUG_LOGIN=true` 会在授权页出现无需密码的员工模拟选择器，仅本机演示使用，默认关闭。后台“MCP 检查”由 admin-api 为所选在职员工签发令牌，真实通过 MCP 校验与改写，不由前端持有签名私钥。离职员工不能签发令牌，可在 SQL 工作台验证其空范围。

按已有 platform 设计，授权凭据可能为部署级共享；本仓库无法控制 platform 的凭据隔离方式，也尚未完成外部 platform 的实际联调。

## 控制台与部署边界

控制台提供全量数据浏览、SQL 工作台、改写配置和 MCP 检查；它与后台 API 是本机管理员调试工具，**没有后台登录认证**，不要直接暴露到公网。部署时通过反向代理仅公开 `/oauth/*`、`/.well-known/*` 与 MCP 端点，将 `/api/*`、开发前端限制在本机或受保护管理网络。

统一演示密码用于本次演示，正式部署应接入员工密码哈希或企业身份系统。OAuth 注册提供 `token_endpoint_auth_method=none` 的公共客户端；不支持客户端密钥、OneID 或客户本人登录。更换签名私钥会使已签发 JWT 失效（JWKS 缓存最长 5 分钟）；OAuth 状态库与私钥必须妥善保留。

## 验证

`make test` 为每个测试进程生成独立临时库，覆盖：全员权限与独立 Go 参考算法、四个时间边界、跨机构客户、员工可见性、数据一致性、SQL 拒绝规则、HTTP MCP 协议、OAuth 登录 / PKCE / CSRF / 授权码重放 / 刷新轮换及重用 / 无效 JWT / 管理员调试权限，以及前端 TypeScript 类型检查。

## 登录故障排查

密码不匹配时返回“工号或密码错误”。授权页过期、重复提交或浏览器未保存校验 Cookie 时会显示重新发起授权的提示。重复打开的授权页分别持有独立校验 Cookie，互不覆盖。升级后若仍停留在旧错误页，请回到 agent-platform 再次点击“账号授权”，在新页面输入原工号和明文密码。不要重复提交已经使用过的旧表单。

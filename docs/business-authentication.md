# 业务 MCP 认证与 Platform 接入

业务认证页面与 agent-platform 的 MCP 地址导入对应四种模式：

| 页面 | Platform `authMode` | 服务端策略 | 凭据 |
| --- | --- | --- | --- |
| 无需认证 | `none` | `NONE` | 配置的匿名业务身份及能力 |
| SSO / MCP OAuth | `mcp` | `OAUTH` | 标准资源发现、浏览器跳转登录、授权码与 PKCE；企业 SSO 使用 `ENTERPRISE_OAUTH` 提供方 |
| 企业 OneID | `oneid` | `ONEID` | 客户端直接提供自身已有的 ID Token，服务端验签并映射业务身份 |
| Token / API Key | `token` | `TOKEN` 或兼容的 `ACCOUNT` | 手工填写的 Bearer、API Key 或 Basic 账号密码 |

策略枚举保留原有大写存储格式；旧 `ACCOUNT` 配置继续有效，在页面归入 Token 下的账号密码方式。Platform 的 token 表单只保存并发送凭据，不会调用账号接口自动换票。业务认证与平台管理登录分开，新增 ONEID/TOKEN 不用于平台管理登录或管理 MCP。

认证配置优先使用本库 `authentication.access`，其次使用逐库 `policyId` 绑定，最后使用全局业务默认策略。`MCP_AUTH_MODE=oauth` 不能覆盖本库明确保存的 NONE/TOKEN/ONEID 策略。更新 Docker 镜像会保留挂载数据中的策略。

## 导出 gen-agent 连接器

在「数据库配置 → 业务认证」保存认证方式，再打开同一数据库的「MCP 配置」。开启 MCP 并保存后，点击当前地址旁的「下载连接器 ZIP」，即可将下载的 ZIP 导入 gen-agent。导出会重新读取本库生效的地址、认证策略及提供方；地址应能从 gen-agent 访问。

无需认证和 MCP OAuth 由平台按对应模式处理。企业 SSO 导出为 `authMode: mcp`，连接时跳转到 SSO 完成登录鉴权；企业 OneID 导出为 `authMode: oneid`，直接使用客户端自身的 ID Token。静态密钥在导入后的凭据表单填写，连接器自动使用本库配置的请求头。账号密码方式导出为个人业务 Token 表单，可通过本库业务登录页获取 Token。包中仅包含连接器声明、MCP 地址和必要的凭据模板。

## SSO 跳转登录

选择“SSO / MCP OAuth → 企业 SSO（跳转登录）”，配置 `ENTERPRISE_OAUTH` 提供方，也可引用全局同类提供方。数据库仍保存 `mode: OAUTH`；连接器在 Platform 选择 `mcp`。未认证请求返回带 `resource_metadata` 的 401 挑战，资源发现元数据中的 `authorization_servers` 指向配置的企业 Issuer。Platform 继续发现该 Issuer 的授权与令牌端点，打开 SSO 登录页，完成授权码与 PKCE 流程后以访问令牌调用本库。DataScope 通过配置的 UserInfo 接口校验身份，再应用本库业务权限。

企业 Issuer 必须提供 Platform 可发现的授权服务器元数据。配置中的登录授权、Token、UserInfo 地址及 Client ID 同时供 DataScope 自身的企业登录与本人确认使用；连接器的授权与令牌地址由 Platform 从 Issuer 发现。本库业务账号提供方 `DEMO_OAUTH` 则跳转本库登录页。

## OneID

在“企业 OneID → 本库独立配置”填写受信任的签发方 Issuer、客户端 ID Token 的接收方 Audience，并选择 JWKS 地址或 PEM 公钥作为验签来源；也可在全局认证设置中定义 ONEID 提供方并引用。地址要求 HTTPS，本机回环 HTTP 仅用于测试。Platform 选择 `oneid` 并使用 HTTPS 默认端口的完整 MCP 地址，客户端按请求发送自身已有的 ID Token，无需为该数据库跳转 SSO 登录。客户端自身的登录与 Token 更新由客户端管理。

服务端只接受 RS256，校验签名、精确 issuer/audience、必填 exp，以及存在时的 nbf/iat。多 audience 时要求 azp 与配置的 audience 一致。JWKS 模式只从管理员配置的地址获取公钥，不使用令牌中的 URL，不跟随重定向；最多缓存一分钟。公钥过期后获取失败则拒绝认证，未知 kid 不会触发无限刷新。PEM 模式可以在页面导入 `publickey.pem` 或直接粘贴公钥内容，支持单个 2048–8192 位 RSA 的 `PUBLIC KEY`（SPKI）或 `RSA PUBLIC KEY`（PKCS#1），不接受私钥、证书或多段公钥；配置保存公钥正文，不保存客户端文件路径。JWKS 与 PEM 必须且只能配置一种，验签失败不尝试另一来源。PEM 模式由管理员更新公钥，`kid` 不用于另行获取密钥。

默认 `sub` 作为稳定主体，`employee_id` 映射到 SQL 行权限使用的正整数业务身份。姓名默认读取 `name`。可配置 `subjectClaim`、`usernameClaim`、`businessIdClaim`；例如员工编号放在其他 JWT 字段时填写该字段名。缺少合法业务身份时拒绝访问，不回退到匿名或平台管理员。签发方需要提供可映射的业务员工身份；只有姓名或邮箱的 ID Token 不能直接作为数字业务身份。若企业身份与业务库内部编号不同，可在本库策略 `authentication.access.businessIdMappings` 明确配置映射（企业身份原值 → 正整数本库员工编号）。启用非空映射后，它是允许列表：未登记的身份拒绝访问，不回退为直接编号；映射键保留原文，包括前导零。空映射保持旧行为，直接解析 `businessIdClaim` 为正整数。映射只适用于 OneID；权限仍由本库策略及业务库的员工/范围表决定。能力来自本库配置，不采用令牌中的 permissions。JWT 有效期内的身份撤销由签发方有效期及本库策略控制；没有在线 UserInfo 撤销查询。

```yaml
authentication:
  type: local
  source: DATABASE
  access:
    id: database-auth
    name: 本库业务认证
    mode: ONEID
    providerId: desktop-oneid
    permissions: [query]
  provider:
    id: desktop-oneid
    name: Desktop 企业身份
    type: ONEID
    enabled: true
    issuer: https://identity.example.com
    jwksUrl: https://identity.example.com/oidc/jwks
    audience: desktop-client
    subjectClaim: sub
    businessIdClaim: employee_id
```


例如签发方提供 `staffno`，但本库使用内部编号39：

```yaml
# authentication.access 下：
businessIdMappings:
  "E1001": 39
# authentication.provider 下：
subjectClaim: sub
usernameClaim: staffno
businessIdClaim: staffno
```

若使用固定PEM公钥，将上面的 `jwksUrl` 删除，改为：

```yaml
publicKeyPem: |
  -----BEGIN PUBLIC KEY-----
  ...实际RSA公钥内容...
  -----END PUBLIC KEY-----
```

签名验证、Issuer/Audience/有效期检查全部成功后，`staffno=E1001` 才映射到员工39。
Token中的姓名、roles、groups不自动授予数据库管理能力或总部权限。
仅解码Token只能读取字段，不能证明签名有效；Token本身不能恢复RSA验签公钥。

## Token / API Key

选择“Token / API Key → 本库独立配置 → Token / API Key”，填写固定的正整数业务身份及能力。保存后点击“生成或重置 API Key”。服务端生成 32 字节随机密钥，只在该次响应中返回原文；关闭弹窗后不能再次查看。重置后旧密钥立即失效，也可直接撤销。

每个数据库独立保存 SHA-256 校验摘要：`data/databases/<ID>/auth/api-key.sha256`。API Key 原文不写入 YAML、`.env` 或审计。认证配置只定义方式、请求头、业务身份与能力，不包含密钥或摘要。摘要文件与目录权限分别为 `0600`、`0700`；删除或损坏摘要后拒绝认证，不回退到其他数据库的密钥。旧部署明确配置的 `secretEnv` 引用保留兼容，点击生成/撤销时会移除本库的旧环境变量引用。

请求头留空时使用 `Authorization: Bearer <密钥>`，连接器选择 Bearer；配置 `X-API-Key` 时，选择 Header 并填写同名请求头。此时 Authorization Bearer 不作为替代入口。

```yaml
authentication:
  type: local
  source: DATABASE
  access:
    id: database-auth
    name: 服务密钥
    mode: TOKEN
    providerId: finance-key
    permissions: [query]
    businessUserId: 10
  provider:
    id: finance-key
    name: Finance API Key
    type: STATIC_TOKEN
    enabled: true
    headerName: X-API-Key
```

普通 API Key 是随机共享秘密，使用摘要比较，不依赖公私钥。登录签发的个人 JWT 则使用 RSA 私钥签名与公钥验签。业务库的密钥对保存在各自 `data/databases/<ID>/auth/signing.pem` 和 `public.pem`，公钥通过 `/auth/databases/<ID>/oauth/jwks.json` 发布；平台管理令牌继续使用平台自己的签名密钥。升级到逐库密钥后，旧业务 Token 需重新登录获取。

账号密码子方式沿用 BUSINESS_ACCOUNT 或 ENTERPRISE_ACCOUNT_API；Platform 可选择 `token` 的 Basic 类型，填写账号密码。也可先从本库账号接口获取短期业务 Token，再手动填入 Platform Bearer 表单。

## 业务工作台与本人确认

业务工作台允许粘贴业务 Bearer/ID Token/API Key，仅在页面内存保留；自定义 API Key 由后端按配置写入对应上游请求头。OneID 本人确认页通过重新验证本人 ID Token，比对原预览主体和配置版本后执行。共享 Token/API Key 不能代替个人确认，需要本人确认的动作须使用账号、OAuth 或 OneID；无需本人确认的动作仍按当前业务权限和写入策略检查。

ONEID/TOKEN 不发布浏览器 OAuth 授权服务器元数据。切换模式后应在 Platform 使用相应模式重新配置连接器；仅 OAuth 模式应发起浏览器授权。

## 业务登录与个人 Token

账号密码方式（`ACCOUNT`）的数据库可使用独立登录页：`/#/business-login?database=<数据库 ID>`。数据库业务认证卡片提供“登录并获取个人 Token”入口，不要求先登录平台管理后台。

页面使用公开的 `GET /auth/databases/<ID>/login-info` 获取数据库名称、MCP 地址和有效期，再调用已有的 `POST /auth/databases/<ID>/token` 验证业务账号密码，签发有效期为一小时的个人 Bearer Token。连接器选择 **Token → Bearer**，粘贴 Token。权限来自当前业务账号；账号停用、密码或权限变更、认证配置变更后，旧 Token 按现有校验机制失效。

密码和 Token 不写入浏览器存储、URL 或平台登录会话。Token 在当前页面内存中保留，刷新、关闭或到期后清除；页面清除不等于服务端撤销。共享 API Key（`TOKEN`）、OAuth 和 OneID 配置不通过此页面换票。使用账号登录时，应在管理后台选择“Token / API Key → 账号密码 / 业务 Token”，并建立该库自己的业务账号。

前端代理需将 `/auth` 转发到管理 API，与 `/api` 的目标相同；本地 Vite 已配置此规则。外部部署应使用 HTTPS。

# 业务 MCP 认证与 Platform 接入

业务认证页面与 agent-platform 的 MCP 地址导入对应四种模式：

| 页面 | Platform `authMode` | 服务端策略 | 凭据 |
| --- | --- | --- | --- |
| 无需认证 | `none` | `NONE` | 配置的匿名业务身份及能力 |
| MCP OAuth | `mcp` | `OAUTH` | 标准资源发现、授权码与 PKCE |
| 企业 SSO | `oneid` | `ONEID` | Desktop 按请求提供的 ID Token |
| Token / API Key | `token` | `TOKEN` 或兼容的 `ACCOUNT` | 手工填写的 Bearer、API Key 或 Basic 账号密码 |

策略枚举保留原有大写存储格式；旧 `ACCOUNT` 配置继续有效，在页面归入 Token 下的账号密码方式。Platform 的 token 表单只保存并发送凭据，不会调用账号接口自动换票。业务认证与平台管理登录分开，新增 ONEID/TOKEN 不用于平台管理登录或管理 MCP。

认证配置优先使用本库 `authentication.access`，其次使用逐库 `policyId` 绑定，最后使用全局业务默认策略。`MCP_AUTH_MODE=oauth` 不能覆盖本库明确保存的 NONE/TOKEN/ONEID 策略。更新 Docker 镜像会保留挂载数据中的策略。

## OneID

在“企业 SSO → 本库独立配置”填写受信任的签发方 Issuer、公钥 JWKS 地址及 Desktop ID Token 的接收方 Audience；也可在全局认证设置中定义 ONEID 提供方并引用。地址要求 HTTPS，本机回环 HTTP 仅用于测试。Platform 选择 `oneid` 并使用 HTTPS 默认端口的完整 MCP 地址，无需额外 OAuth 登录。

服务端只接受 RS256，校验签名、精确 issuer/audience、必填 exp，以及存在时的 nbf/iat。多 audience 时要求 azp 与配置的 audience 一致。公钥只从管理员配置的 JWKS 地址获取，不使用令牌中的 URL，不跟随重定向；最多缓存一分钟。公钥过期后获取失败则拒绝认证，未知 kid 不会触发无限刷新。

默认 `sub` 作为稳定主体，`employee_id` 映射到 SQL 行权限使用的正整数业务身份。可配置 `subjectClaim`、`usernameClaim`、`businessIdClaim`；例如员工编号放在其他 JWT 字段时填写该字段名。缺少合法业务身份时拒绝访问，不回退到匿名或平台管理员。签发方需要提供可映射的业务员工身份；只有姓名或邮箱的 ID Token 不能直接作为数字业务身份。能力来自本库配置，不采用令牌中的 permissions。JWT 有效期内的身份撤销由签发方有效期及本库策略控制；没有在线 UserInfo 撤销查询。

```yaml
authentication:
  type: local
  source: DATABASE
  access:
    id: database-auth
    name: 本库业务认证
    mode: ONEID
    providerId: desktop-sso
    permissions: [query]
  provider:
    id: desktop-sso
    name: Desktop 企业身份
    type: ONEID
    enabled: true
    issuer: https://identity.example.com
    jwksUrl: https://identity.example.com/oidc/jwks
    audience: desktop-client
    subjectClaim: sub
    businessIdClaim: employee_id
```

## Token / API Key

选择“Token / API Key → 本库独立配置 → Token / API Key”，填写密钥环境变量名，例如 `DATASCOPE_AUTH_FINANCE_KEY`，以及固定的正整数业务身份和能力。密钥本身至少 16 字符，由部署环境提供；配置、API 响应、审计均只保存变量名。变量未设置、为空或过短时拒绝所有请求。修改部署环境变量后需重新创建/启动持有该环境的容器。

请求头留空时使用 `Authorization: Bearer <密钥>`，Platform 选择 `token` 的 Bearer 类型。配置 `X-API-Key` 等自定义头时，Platform 选择 Header 类型并填写同一请求头名；此时 Authorization Bearer 不作为替代入口。禁止 Host、Cookie、Authorization、MCP 协议等保留请求头。API Key 使用固定业务身份和当前配置能力，每次请求重新解析策略，配置撤销即时生效。

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
    secretEnv: DATASCOPE_AUTH_FINANCE_KEY
    headerName: X-API-Key
```

账号密码子方式沿用 BUSINESS_ACCOUNT 或 ENTERPRISE_ACCOUNT_API；Platform 可选择 `token` 的 Basic 类型，填写账号密码。也可先从本库账号接口获取短期业务 Token，再手动填入 Platform Bearer 表单。

## 业务工作台与本人确认

业务工作台允许粘贴业务 Bearer/ID Token/API Key，仅在页面内存保留；自定义 API Key 由后端按配置写入对应上游请求头。OneID 本人确认页通过重新验证本人 ID Token，比对原预览主体和配置版本后执行。共享 Token/API Key 不能代替个人确认，需要本人确认的动作须使用账号、OAuth 或 OneID；无需本人确认的动作仍按当前业务权限和写入策略检查。

ONEID/TOKEN 不发布浏览器 OAuth 授权服务器元数据。切换模式后应在 Platform 使用相应模式重新配置连接器；仅 OAuth 模式应发起浏览器授权。

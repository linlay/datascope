# 跨 Schema 验收

## 自动化

```sh
go test ./...
go test ./internal/store ./internal/dialect ./internal/catalog -run 'Schema|CrossSchema|SameName' -v
cd web
npm run build
```

浏览器表单和选表验收使用模拟 API，不连接真实数据库：

```sh
DATASCOPE_PLAYWRIGHT=/path/to/playwright \
DATASCOPE_CHROMIUM=/path/to/chrome \
node scripts/test-schema-configuration-browser.cjs
```

覆盖 Oracle 默认 Schema 可空、Service name 必填、切换驱动后默认端口、零表连接成功、Schema 范围保存、候选表搜索分页、显式选表登记和新表默认关闭。

`internal/dialect/schema_join_test.go` 在独立 SQLite 参考数据上执行四种网络方言生成的 JOIN，验证行/字段权限和 JOIN 结果；这不代表真实厂商数据库验证。`internal/store/schema_mcp_test.go` 使用协议模拟连接和真实 MCP 内存传输，验证 Oracle OWNER、候选与开放隔离、预览及拒绝查询不触达业务驱动。`schema_scope_test.go` 验证独立脱敏规则、迁移与范围移除。

## 真实数据库

管理员在隔离测试实例创建 `ds_sales` / `ds_crm`，Oracle/DM 使用大写 `DS_SALES` / `DS_CRM`。为只读登录 `ds_join_reader`（Oracle/DM 为 `DS_JOIN_READER`）配置登录权限和密码，再执行 [测试数据脚本](../fixtures/databases/schema_join/)。Oracle 读账号自身不创建业务表，只通过授权读取两个 owner 的同名 RECORDS。脚本不自动执行，也不覆盖已有同名对象；重建仅限这些隔离测试 Schema。

设置对应密码环境变量，以及连接 JSON，例如 MySQL：

```sh
export DATASCOPE_DB_SCHEMA_TEST_PASSWORD='测试账号密码'
export DATASCOPE_SCHEMA_INTEGRATION_MYSQL='{"connection":{"host":"127.0.0.1","port":3306,"username":"ds_join_reader","password_env":"DATASCOPE_DB_SCHEMA_TEST_PASSWORD","tls_mode":"disable"},"first_schema":"ds_sales","second_schema":"ds_crm"}'
go test ./internal/store -run TestSchemaJoinNetworkIntegration -v -count=1
```

其他变量为 `DATASCOPE_SCHEMA_INTEGRATION_POSTGRESQL`、`DATASCOPE_SCHEMA_INTEGRATION_ORACLE`、`DATASCOPE_SCHEMA_INTEGRATION_DAMENG`。PG connection 仍需 database；Oracle connection 仍需 service_name；端口必须提供。Oracle/DM 的 first_schema、second_schema 使用实际大写名称。测试主动清空默认 Schema，配置两个允许范围；不运行建表、授权或写入语句。

验收覆盖：同名表搜索与分页；选择一张后快照、预览和 MCP 正确命中，另一张拒绝；跨 Schema 普通/别名/子查询/CTE JOIN 返回 Alice；禁止字段和未登记表拒绝；关闭参与表后 JOIN 拒绝；移除 Schema 后 raw_query 也不能越过范围；指定单 Schema 后只发现该 Schema。

原单 Schema 网络回归使用 `DATASCOPE_INTEGRATION_<DRIVER>`，执行 `go test ./internal/store -run TestNetworkDatabaseIntegration -v -count=1`。

## 本次验证记录（2026-11-06）

- 完整 Go 回归、跨 Schema 自动化、前端构建及 16 项连接器测试通过。
- 本机 Chrome 的模拟 API 浏览器验收通过，包含上述表单和选表流程。
- 已尝试项目 `.env` 中 MYSQL_TEST 配置的只读连接探测：网络连接失败，未执行真实跨库 JOIN。
- Oracle、PostgreSQL、达梦未提供真实验收连接；对应集成测试显示 SKIP。不能计为真实数据库验收通过。
- 已提供四种数据库的全新验收数据脚本；现有 SQLite demo 的回归通过。SQLite 继续固定单库，不要求通过网络数据库验证。

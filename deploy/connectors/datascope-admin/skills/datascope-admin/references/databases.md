# 数据库与 MCP

所有调用使用 `{action,args}`；下表参数均位于 args，另加对应 `resourceType`。除列出的可选参数外，其余参数必需；不接受 null 或无关参数。数字使用 JSON number，布尔值使用 true/false。

| 工具 | resourceType | action | 参数 | 权限 |
| --- | --- | --- | --- | --- |
| datascope_query | database | list | 无 | 返回当前用户有权限的数据库 |
| datascope_query | database | get | database_id | config.read |
| datascope_manage | database | create | database | databases.create |
| datascope_manage | database | update | database_id、database | config.read + config.write，仅改名称 |
| datascope_manage | database | save | database_id、database | config.read + 实际变更部分的权限 |
| datascope_manage | mcp | save | database_id、database | config.read + mcp.write |
| datascope_inspect | database | test_connection | database_id | config.read |

发现数据库：

```json
{"action":"list","args":{"resourceType":"database"}}
```

读取配置：

```json
{"action":"get","args":{"resourceType":"database","database_id":"default"}}
```

读取结果将数据库字段直接放在返回对象顶层，并附带 mcp_endpoint、permissions、business_user_id。构造 `args.database` 时只复制数据库字段：id、name、driver、path、mcp_enabled、mcp_path、tools、business_tools、config、revision，以及返回时存在的 etag。不要把整份返回对象直接作为 database 传回。

创建数据库只需传 `database:{id,name,driver:"sqlite",path}`，path 是 DataScope 服务端已有 SQLite 文件的路径，必须在服务端允许的数据根内。客户端本机路径不一定属于服务端。创建后服务端生成策略，默认关闭 MCP、禁用所有表、清空基础工具；回读实际配置再准备策略，不能假设传入策略已生效。

已有数据库修改时，database.id 必须等于 database_id，database.revision 必须是最近回读的正整数版本。update 只采用 database.name；mcp.save 只采用 database 中的 mcp_enabled、mcp_path、tools、business_tools；policy.save 见策略文档。其他字段不随局部动作修改。使用完整 database.save 时保留当前配置，服务端按名称、策略、MCP 部分的实际差异逐项检查 config.write/policy.write/mcp.write；文件路径与驱动保持原值。

配置 MCP 前先完成策略并验证。基础 tools 允许 list_tables、describe_table、query、raw_query、customer_scope、search_knowledge、read_knowledge；raw_query 绕过行字段策略且需要单独权限，customer_scope 仅适用于证券样例库，按用户需要选择。mcp_path 是地址路径段，admin 为保留名称。不要在没有用户需求时开放全量查询。

业务工具使用 business_tools 数组，每项为 `{name,description,sql,parameters}`。parameters 为参数名到 `{type,description}` 的映射；type 为 string/integer/number/boolean，SQL 使用 `:arg_参数名` 绑定，每个声明参数都必须被使用。业务 SQL 继续受服务端改写约束。

保存后重新 database.get 核对 revision 和目标字段。test_connection 检查保存配置下的连接和策略，不证明 OAuth 登录、所有业务 SQL 或数据库 MCP 均可用。管理地址的授权不能复用于数据库地址。

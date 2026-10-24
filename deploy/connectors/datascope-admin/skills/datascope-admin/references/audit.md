# 审计

使用 datascope_query，action:"search"，args.resourceType:"audit"。可选参数：database_id、request_id、limit、offset。所有省略参数都留空，不传 null；limit/offset 使用非负 JSON 整数。

```json
{"action":"search","args":{"resourceType":"audit","database_id":"default","request_id":"真实请求ID","limit":100,"offset":0}}
```

指定 database_id 时需要该库 audit.read；省略 database_id 的全局审计仅系统管理员可查。limit 为 1–200，省略或 0 使用 100，超过 200 服务端回退到 100。结果按时间倒序，为事件数组，返回不足实际页大小时已到当前末尾；offset 翻页期间可能有新事件，不保证一致快照。

用 request_id 关联管理动作、SQL 原文、改写和执行事件。kind/tool 保留底层业务动作名，例如 action.save_policy，而不是统一变成 datascope_manage，便于定位具体操作。没有事件不一定说明未执行：先检查数据库和请求过滤范围、分页及权限。status:unknown 表示结果不确定，结合当前配置回读，不直接重试写入。

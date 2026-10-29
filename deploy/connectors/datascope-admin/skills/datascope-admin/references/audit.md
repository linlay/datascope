# 操作日志

使用 `datascope_query`，`args.resourceType:"audit"`：

- `action:"search"`：保持原事件数组响应。
- `action:"page"`：返回 `{items,total,limit,offset,kinds,sources,actors,actor_names}`，用于筛选和分页。

可选参数：`database_id`、`platform_only`、`request_id`、`actor_id`、`kind`、`source`、`status`、`tool`、`query`、`started_from`、`started_to`、`limit`、`offset`。不传 null；actor_id/limit/offset 使用非负 JSON 整数，actor_id=0 为系统任务。平台日志筛选 platform_only=true 不能同时指定 database_id。

```json
{"action":"page","args":{"resourceType":"audit","database_id":"default","status":"failed","source":"web","started_from":"2026-10-28T00:00:00+08:00","started_to":"2026-10-28T23:59:59+08:00","limit":50,"offset":0}}
```

指定 database_id 时需要该库 audit.read；省略 database_id 或使用 platform_only 的全局范围仅系统管理员可查。筛选项、记录和总数都限制在同一授权范围。kind/source/tool/request_id 为精确匹配；query 按普通文本搜索事件中的 SQL、错误和详情，不解析成 SQL。时间范围接受 RFC3339（含时区），两端均包含。status 可为 success、failed、running、unknown，超过两分钟仍为 running 的事件按 unknown 返回和筛选。

limit 为 1–200，省略或 0 使用 100，超过 200 回退到 100。结果按时间倒序；筛选、总数和当前分页采用同一读取事务。跨页期间可能产生新事件，不保证跨请求的一致快照。

用 request_id 关联管理动作、SQL 原文、改写和执行事件。kind/tool 保留底层业务动作名，例如 action.save_policy。没有事件不一定说明未执行：先核对过滤范围、分页和权限。unknown 表示结果不确定，应先回读配置，不直接重试写入。

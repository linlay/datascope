# 业务知识

全部调用使用 resourceType:"knowledge"，且需要 database_id。

| 工具 | action | 其他必需参数 | 可选参数 | 语义与权限 |
| --- | --- | --- | --- | --- |
| datascope_query | list | 无 | query | 管理摘要，含草稿，knowledge.write |
| datascope_query | get | id | 无 | 管理完整正文，含草稿，knowledge.write |
| datascope_query | search | query | 无 | 已发布可见摘要，knowledge.read + query 权限 |
| datascope_query | read | id | 无 | 已发布可见正文，knowledge.read + query 权限 |
| datascope_manage | save | id、name、summary、content、tables、published、revision | 无 | 创建或完整更新，knowledge.write |

create 与 update 统一使用 save；新知识 revision:0，已有知识必须传 get 返回的最新 revision。save 完整替换该条知识，保留未要求修改的字段，显式提供 published:true/false，不能遗漏后意外下架。summary/content 可为空字符串，tables 可为空数组，正文最多 200000 字节，id 非空且不能含斜杠，tables 中的表必须存在于当前策略。

创建草稿示例：

```json
{"action":"save","args":{"resourceType":"knowledge","database_id":"default","id":"join-rules","name":"关联规则","summary":"订单明细关联时避免重复汇总","content":"汇总前按业务主键去重。","tables":[],"published":false,"revision":0}}
```

search/list 最多返回 50 条摘要；不支持 limit/offset，不能声称是完整大规模枚举。get 与 read 的可见范围不同，不因 read 查不到就断言不存在。已发布读取还受关联表可见性约束，字段受限或禁用的表可能令知识不可见。

写后用 get 验证正文、状态和 revision；仅在用户要求发布时设为 true。发布状态不意味着所有查询用户均可读取，读取权限继续由服务端判断。

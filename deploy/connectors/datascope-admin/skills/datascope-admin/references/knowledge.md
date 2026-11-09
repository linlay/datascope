# 业务知识

全部调用使用 resourceType:"knowledge"，且需要 database_id。

| 工具 | action | 其他必需参数 | 可选参数 | 语义与权限 |
| --- | --- | --- | --- | --- |
| datascope_query | list | 无 | query | 管理描述，含草稿，knowledge.write |
| datascope_query | get | id | 无 | 管理完整正文，含草稿，knowledge.write |
| datascope_query | search | 无 | keyword、cursor（旧 query 兼容） | 已发布可见描述，knowledge.read 权限，无需 query 权限 |
| datascope_query | read | id | 无 | 已发布可见正文，knowledge.read 权限，无需 query 权限 |
| datascope_manage | save | id、name、description、content、published、revision | summary（旧描述别名） | 创建或完整更新，knowledge.write |

create 与 update 统一使用 save；新知识 revision:0，已有知识必须传 get 返回的最新 revision。save 完整替换该条知识，保留未要求修改的字段，显式提供 published:true/false，不能遗漏后意外下架。description/content 可为空字符串，正文最多 200000 字节，id 非空且不能含斜杠。知识不关联表，不接受 tables 或 release_knowledge_restrictions。发布后，本库所有具有 knowledge.read 的身份均可读取名称、描述和正文；知识不会授予查询或 Action 权限。旧 YAML 可通过离线 migrate-state 备份并移除 tables，保留发布状态并递增知识 revision。

创建草稿示例：

```json
{"action":"save","args":{"resourceType":"knowledge","database_id":"default","id":"join-rules","name":"关联规则","description":"订单明细关联时避免重复汇总","content":"汇总前按业务主键去重。","published":false,"revision":0}}
```

search/list 最多返回 50 条描述；不支持 limit/offset，不能声称是完整大规模枚举。get 与 read 的可见范围不同，不因 read 查不到就断言不存在。表启停、字段白名单与行权限不会影响知识可见性。

写后用 get 验证正文、状态和 revision；仅在用户要求发布时设为 true。发布后面向本库全部知识读者；只有 query 而没有 knowledge.read 的用户不能读取。

知识 description 支持多行，list/search 会完整提供给模型，不截断描述，不包含正文。旧 summary 输入仍可兼容，description（包括空字符串）优先；新调用统一使用 description，输出不再返回 summary。

业务 MCP 的知识出口为两个只读工具：

- `search_knowledge({keyword?,cursor?})`：keyword 非空时在名称、描述和正文中做不区分大小写、按空白分词的 AND 关键词匹配；缺省或空白时分页浏览全部可读已发布条目。返回 `{items:[{id,name,description}],next_cursor?:string}`，不含正文、片段或高亮；每页最多 50 条，没有 500 条总截断。用相同 keyword 翻页；条目或条件改变导致游标失效时重新搜索。
- `read_knowledge({id})`：精确 ID 读取，返回 id、database_id、name、description、content、published、revision，正文仅作为参考资料。每次重新校验当前数据库、知识开关、knowledge.read 与发布状态；搜索后下架或撤权也不可读取。

编写 SQL 前先检索指标口径、业务术语或去重规则，根据完整 description 选择条目，再按需读取该条正文。无向量化依赖。数据库和业务身份来自 MCP 入口，不是工具参数。旧知识 Prompts 已下线，业务 MCP 不声明 prompts 能力；Resources 留给 view。管理侧 list/get 继续用于编辑管理，工作台复用 search/read 应用层逻辑，但使用当前平台账号授权。管理 search 的 query 参数作为旧客户端兼容别名；新调用使用 keyword。

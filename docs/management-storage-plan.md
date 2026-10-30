# 业务管理配置下一阶段方案

状态：需求与目录方案已确定，尚未落地。当前运行版本仍使用全局及逐库 SQLite 管理存储；知识工作台仍通过 search_knowledge / read_knowledge 模拟调用。提交中的现有功能不能视为以下改造已完成。

## 存储与目录

全局 `data/management.db` 继续使用 SQLite，维护平台用户、全局认证与数据源目录。业务数据库结构和数据保持原有规范。各业务库后续将可维护配置从 management.db 迁出，按下列目录组织 YAML：

```text
data/
  management.db
  audit.db
  databases/<database_id>/
    management/
      database.yml
      authentication.yml
      account.yml
      grant.yml
      tables/<table>.yml
      actions/<action>.yml
      knowledges/<knowledge_id>.yml
    audit.db
    credentials.key
```

字段遵循大写、下划线分隔且末尾带下划线，如 ID_、NAME_、DESCRIPTION_、CONTENT_、REVISION_。业务标识与参数名保持自身业务语义。SQL、JavaScript 和知识正文使用 YAML 多行文本。

不设 template/、公共 SQL fragment、version/、state/ 或独立 prompts/ 目录。每张表保存完整权限 SQL，允许重复；旧片段引用在迁移时展开并保留参数绑定。SQL 直接编写并可绑定变量，绑定变量不构成独立查询模板需求。DML 通过 Action 执行，Action 内可以 SELECT 做校验。已有 business_tools 属于旧实现，不能作为本轮新增模板需求的依据，其入口与配置清理属于后续迁移工作。

保留当前 REVISION_ 做并发保存检查，不生成历史版本目录。已登记结构放入对应表文件，派生索引和缓存在内存重建。账号密码保留哈希，连接密码保留加密方式，credentials.key 继续独立存放。迁移必须先备份、逐项校验并保留可恢复的原文件。

## 知识复用 MCP Prompts

统一从 knowledges/ 维护知识，列表展示完整、多行 description，正文按需读取：

- prompts/list：name 使用知识 ID，title 使用显示名称，description 使用完整知识描述，不返回正文；按协议分页。
- prompts/get(name)：读取同一份正文，包装为 user 文本消息；一期无模板参数。
- 仅公开已发布且当前业务身份有权读取的知识；列表和读取均执行当前库的身份与权限检查。
- 知识工作台统一采用列出、选择和获取正文流程，展示请求、响应与耗时。
- Resources 留给后续 view，不作为本轮知识出口；现有 search_knowledge / read_knowledge 的迁移与退出需要一并实现。

## 配置克隆

支持从源业务库克隆 management 配置到目标业务库：默认复制表与字段配置、查询权限、Action 和知识；认证设置作为明确可选项。目标库保留自己的连接、账号密码、管理授权、身份绑定、MCP 路由、审计和动作回执。

克隆前展示覆盖范围、检查源和目标配置版本及结构兼容性；整组保存必须保证一致性。知识 ID 可以保留，数据库归属使用目标 ID。配置克隆不等于复制业务数据或扩大账号授权。

## 动作幂等回执

回执是系统运行状态，不属于业务 YAML 配置，也不随配置克隆。后续由全局 management.db 的 ACTION_RECEIPT 系统表统一持久保存，按 DATABASE_ID_、ACTOR_、REQUEST_ID_ 隔离。此项需要迁移现有逐库回执并调整全局表初始化，不能仅新建空表后丢弃旧记录。

执行前的幂等登记必须持久成功；重试返回已保存结果，执行中或结果未知的请求禁止自动重放。内存缓存不能替代跨进程、跨重启的持久回执。

audit.db 保持独立且写入失败不阻止业务执行。不能把执行必需的幂等登记放入可丢弃的审计流程；全局回执、业务数据和必要密钥须纳入一致的备份恢复方案。

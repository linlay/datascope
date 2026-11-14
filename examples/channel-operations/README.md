# 渠道与社交媒体运营演示数据库

面向使用者的描述和使用说明维护在 `bundle.yml` 中，导入后可在控制台点击“使用说明”阅读。正文提供自然语言提问、连续追问、角色视角、业务操作与结果解读；本 README 保留创建、维护和回归说明。

虚构双品牌「原野生活」「山岚营地」运营库：4 个模拟平台、12 个账号、20 名成员、30 个活动、600 个选题和 1,600 次发布任务。覆盖 2026 年 5–10 月运营历史及 11 月近期任务，默认快照 2026-11-13。约 22 MiB，包含数万条每日指标、原始快照、评论与处置记录；实际大小随种子变化。平台口径为合成示例，不代表真实平台接口。

## 创建与导入

在仓库根目录执行，生成仅依赖 Python 标准库，拒绝覆盖已有文件：

```sh
make init-channel-operations
# 指定新输出及固定种子：
python3 examples/channel-operations/generate.py --db /tmp/channel-operations.sqlite3 --seed 20261113

# 设置 DATASCOPE_EXAMPLE_PASSWORD，至少 12 字符，再显式校验和导入：
make check-example EXAMPLE=channel-operations
make import-example EXAMPLE=channel-operations
```

导入数据库 ID 为 channel_operations_demo，包含 OPS001 管理员、OPS002 负责人、OPS003 分析员、OPS004 编辑、OPS005 运营、OPS006 外部协作和 OPS007 另一团队负责人。初始密码从环境变量读取，配置包不保存密码。已有登记和账号不会被重复导入覆盖；主服务及生产镜像不会自动生成或安装示例。所有姓名、评论、素材和转化均为虚构数据。

## 文件

- `schema.sqlite.sql`：25 张表，包含中文结构注释表，使用外键、状态检查及唯一索引。
- `generate.py`：确定性造数、22 项一致性检查、JSON 规模报告及失败清理。
- `rewrite.yml`：账号有效授权、团队和选题分工、推广成本与外部协作隔离。
- `actions.yml`：六个需本人确认的本地业务操作，包含公共身份与账号授权库。
- `sql-examples.yml`：十个查询，包括内容排行、指标缺失与修订、最新关注存量、推广效果、未知归因及评论待办。
- `guide.md`：数据关系、指标限制、流程和权限，作为业务知识文档导入；面向使用者的使用说明单独维护在 `bundle.yml`。

原始累计快照禁止跨日求和，也不能直接差分为每日指标；每日指标有独立报告口径。缺失为 NULL，真实零为 0。平台 C 不提供自然/付费拆分；平台报告转化和站内七天最后点击归因分别记录，未知渠道不强行归因，重复或无效线索不计入有效转化。评论是运营抽样，非完整归档。

在「查询工作台」选定身份检查账号、内容与效果范围；在「动作工作台」验证提交、审核、排期、模拟发布结果、评论处置和编辑分工。Action 只操作本地数据库，不发送评论、不连接平台、不执行外部发布。未来排期不能提前记录成功；发布失败及下架历史均保留。READ 授权不授予操作权限，选题分工不授予账号权限。

## 回归

```sh
python3 -m unittest discover -s tests/examples -p 'channel_operations_test.py'
go test ./tests/examples/channel-operations ./internal/configbundle ./tests/architecture -count=1
```

测试使用隔离数据库，验证角色和账号授权、到期边界、跨团队访问、成本隐藏、SQL 示例、正常操作、重复状态迁移、预览回滚及确认提交。不会写入本地演示业务库。

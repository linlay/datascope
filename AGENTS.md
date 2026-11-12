# 推送前的 Dockerfile 约定

- 推送本工程前，按目标远程仓库的 URL 确认平台。内网 GitLab 使用内网版作为根目录的 `Dockerfile`，公网版保留为 `Dockerfile.public`。GitHub 使用公网版作为根目录的 `Dockerfile`，内网版保留为 `Dockerfile.internal`。
- 根据 `FROM` 镜像地址识别版本（内网为 `harbor.gtjaqh.io`，公网为 `docker.io`）。保留两个版本的内容和本地修改，并检查待推送提交中的文件名及内容。向两个平台推送时分别准备对应的提交。

# 源码与业务示例边界

- `internal/` 只放通用技术实现，不放业务演示的生成器、安装逻辑、业务权限算法、Action 定义、业务 HTML 视图或业务说明。
- 业务示例按领域集中在 `examples/<名称>/`，只保存 SQLite/SQL、声明式配置、文档和可选的 Python 造数脚本；不得包含 Go 包或专用 Go 安装器。
- `internal/`、`cmd/` 的非测试源码不得导入 `examples/` 或 `tests/`。主服务和生产镜像不得自动生成示例或初始化演示账号；需要导入时显式使用通用配置导入器。通用接口不能硬编码演示库的业务 SQL 或员工编号。
- 跨包测试辅助代码放在 `tests/testutil/`；驱动和 SQL 语法等技术测试数据可以放在 `fixtures/`。实际数据库、账号和认证配置保留在被 Git 忽略的 `data/`。

# 用户帮助文档

- 变更用户可见的功能、入口、操作步骤、权限、参数、返回结果或脚本接口时，检查并在同一变更中同步更新前台使用手册。中英文源文件位于 web/src/help/guide.zh-CN.md 和 web/src/help/guide.en.md，保持章节顺序和示例一致。
- 核对受影响的帮助入口、示例和界面名称。动作脚本契约发生变化时，同时核对 docs/actions.md。

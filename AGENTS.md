# 推送前的 Dockerfile 约定

- 推送本工程前，按目标远程仓库的 URL 确认平台。内网 GitLab 使用内网版作为根目录的 `Dockerfile`，公网版保留为 `Dockerfile.public`。GitHub 使用公网版作为根目录的 `Dockerfile`，内网版保留为 `Dockerfile.internal`。
- 根据 `FROM` 镜像地址识别版本（内网为 `harbor.gtjaqh.io`，公网为 `docker.io`）。保留两个版本的内容和本地修改，并检查待推送提交中的文件名及内容。向两个平台推送时分别准备对应的提交。

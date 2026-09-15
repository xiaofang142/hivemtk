# 贡献指南 (Contributing)

> 本仓库采用 [AGPL-3.0 开源协议](LICENSE)，欢迎自由使用、私有部署与二次开发，并接受社区贡献。请注意：若你将修改后的版本通过网络（SaaS / 云端 / API 等）对外提供服务，须按 AGPL-3.0 向用户开源你的修改（详见 NOTICE 与 LICENSE 第 13 条）。

## 1. 适用范围

本仓库为 HiveMtk 用户端（hivemtk）源代码仓库，**向所有开发者开放**：
- 希望使用、学习或二次开发本项目的开发者
- 希望贡献代码的社区成员
- 需要私有化部署或二次开发支持的团队

欢迎提交 PR 与 Issue，请遵循本指南的开发流程。

## 2. 开发流程

### 2.1 本地开发环境

- Go 1.25.0
- Node.js 20 LTS
- Docker 24+ & Docker Compose v2
- PostgreSQL 15+（或使用项目自带 docker 容器）

### 2.2 启动本地栈

```bash
# 1. 复制环境变量
cp .env-example .env
# 编辑 .env，至少设置：
#   POSTGRES_PASSWORD、REDIS_PASSWORD、JWT_SECRET

# 2. 启动本地推理栈
make inference-host-install   # 首次安装 llama.cpp（宿主机推理栈）
make inference-host-models    # 下载 dev 档模型（Qwen2.5-3B + bge-m3 + bge-reranker-v2-m3）
make inference-host-up        # 拉起宿主机 llama-server :8207 (LLM) / :8208 (Embedding) / :8209 (Rerank)

# 3. 构建前端
make web-build              # user-web
make sdk-build              # embed-sdk

# 4. 启动用户端
make up

# 5. 健康检查
curl http://localhost:8204/health
```

### 2.3 代码规范

- 后端 Go 代码：遵循 [docs/architecture/GO_FIVE_LAYER_ARCHITECTURE.md](docs/architecture/GO_FIVE_LAYER_ARCHITECTURE.md) 五层架构规范
- 前端 Vue 代码：使用 JavaScript（禁止 TypeScript），遵循 Element Plus 风格
- 五层架构：Controller → Service → Repository → Model → DTO，**禁止跨层调用**（CI 静态检查：scripts/check-architecture.sh）

### 2.4 测试

- 后端 API：`make test-go`（= `cd user-server && go test ./... -count=1`）+ `scripts/regression_test.sh`（端到端 smoke）
- 前端 UI：使用 Playwright，详见 `tests/ui/user/`
- 推理栈连通性：`make inference-host-test`（宿主机推理栈端到端 smoke test）

### 2.5 提交流程

1. 从 `main` 创建特性分支：`git checkout -b feature/<name>`
2. 提交信息遵循 [Conventional Commits](https://www.conventionalcommits.org/zh-hans/)
3. 推送前自测：`make inference-host-test`（推理栈端到端 smoke test）
4. 推送并创建 Merge Request

## 3. 反馈与支持

- 技术问题：通过 Gitee / GitHub Issue 反馈
- 安全漏洞：请私下联系 `jideilvluoqun@gmail.com`（详见 [SECURITY.md](SECURITY.md)）

## 4. 行为准则

- 不接受任何形式的不当言论、骚扰、歧视
- 提交内容须符合中华人民共和国法律法规
- 请勿提交包含第三方商业秘密或违反开源协议的代码

## 5. 相关仓库

- 平台端：[hivemtk-platform](https://gitee.com/xhpmayun/hivemtk-platform)
- 历史合并仓库：[marketing-tools-kit](https://gitee.com/xhpmayun/marketing-tools-kit)（已归档）

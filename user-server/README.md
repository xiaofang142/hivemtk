# user-server · 用户端后端服务

> HiveMtk 的 Go 后端核心服务：多渠道 CDP、ReAct 智能体引擎、RAG 知识库、智能卡片、触达编排都从这里出。覆盖用户端 94 个核心业务模块，Go 1.25 / Gin / GORM,所有客户业务数据本地化存储。项目总览见 [仓库根 README](../README.md)。
>
> 推理栈架构：**宿主机 llama.cpp + TEI 兼容服务**（非容器化），数据层（PostgreSQL + Redis）走 Docker。详见 [`../docs/architecture/HOST_INFERENCE_PLAN.md`](../docs/architecture/HOST_INFERENCE_PLAN.md)。
>
> 🚀 **开发体验**：项目内置 [air 热重载](docs/dev/HOT_RELOAD.md)，`make dev` 一行启动，保存 .go / .yaml / .env 即自动重编+重启，**无需手动 `go build` / `docker compose restart`**。

## ✨ 项目功能

### 核心业务能力

- **多渠道 CDP 客户数据平台**：抖音 / 快手 / 小红书 / 闲鱼 / 微信 / 短信 / 邮件 / WhatsApp / Telegram 统一接入与 OneID 合并
- **AI 智能体客服（ReAct 引擎）**：[41 个原子工具](../docs/architecture/agent-tools-inventory.md)（触达 / 项目管理 / 客户 / 知识库 / 业务）的多智能体编排
- **RAG 知识库**：pgvector 1024 维向量检索 + BGE Reranker 精排 + Redis LRU 双层缓存
- **智能卡片**：抖音 / 快手 / 小红书 / 闲鱼 / TikTok 五平台卡券生成 + 短链追踪 + 转化漏斗
- **SOP 自动化营销**：AB 实验、销冠 Prompt 库、营销画布、用户旅程编排
- **私域触达**：活码（短链 + 二维码轮询）、短信 / 邮件 / WhatsApp / 微信群发、Webhook 回调
- **数据看板**：实时 SSE 推送、客户 360°、RFM 分群、自定义报表、转化漏斗
- **平台对接**：心跳上报、安装信息回传、统计上报（开源版无 License 管理功能 / 无 OTA 升级）

### 平台与安全能力

- **认证与权限**：JWT + MFA（双因素）、细粒度 RBAC、操作审计、敏感数据脱敏日志
- **可观测性**：TraceID 全链路追踪 + 健康检查（`/health` `/healthz` `/readyz`）；私域无外部监控端点，关键指标落库 `layer_decision_logs` / `audit_logs`，巡检通过 SQL + `scripts/post_deploy_check.sh`
- **限流与防滥用**：IP 限流、API Key 限流、登录暴力破解防御
- **运营能力**：数据库备份 / 恢复、域名池健康巡检、活码轮询、活码统计

### 工程能力

- **迁移系统**：内建 migration registry，初始化时按序执行 `migrations/*.sql`
- **配置热加载**：`config.yaml` 支持 `${ENV_VAR}` 占位符注入敏感字段
- **宿主机推理栈**：LLM / Embedding / Rerank 通过三个 `llama-server` 进程暴露 OpenAI 兼容 / TEI 兼容接口，user-server 仅以 HTTP 客户端方式调用

## 🧱 技术栈

| 维度 | 选型 |
|---|---|
| 语言 | Go 1.25 |
| Web 框架 | [Gin](https://github.com/gin-gonic/gin) v1.9 |
| ORM | [GORM](https://gorm.io) v1.30 + pgx/v5 |
| 数据库 | PostgreSQL 16 + pgvector（向量库同库） |
| 缓存 | Redis 7（go-redis v9） |
| 向量检索 | pgvector 1024 维 |
| Embedding | bge-m3（1024 维，TEI 兼容 / `llama-server`） |
| Rerank | bge-reranker-v2-m3（TEI 兼容 / `llama-server`） |
| LLM（dev 档） | Qwen2.5-3B-Instruct（Q4_K_M，`llama.cpp` 启动） |
| LLM（prod 档） | Qwen2.5-14B-Instruct（Q4_K_M，`llama.cpp` 启动） |
| 日志 | zerolog |
| 监控 | TraceID 中间件 + 应用层日志（私域无外部监控端点） |
| API 文档 | swaggo/gin-swagger |
| 浏览器自动化 | chromedp（卡片自动回复） |

> dev / prod 档完整模型定义见 [`../scripts/inference-host/models.env`](../scripts/inference-host/models.env)。

## 🧱 架构分层

严格遵循 **Controller → Service → Repository → Model → DTO** 分层，禁止跨层调用。完整规范见 [`../docs/standards/MASTER_RULES.md`](../docs/standards/MASTER_RULES.md)。

| 层 | 目录 | 职责 | 依赖方向 |
|---|---|---|---|
| Controller | `internal/controller/` | HTTP 入参解析、鉴权、调用 Service、出参封装 | → Service |
| Service | `internal/service/` | 业务编排、事务、领域逻辑、调用 Repository | → Repository / DTO |
| Repository | `internal/repository/` | 数据访问（GORM）、查询封装、聚合根加载 | → Model |
| Model | `internal/model/` | GORM 实体定义、表结构映射 | 无下游 |
| DTO | `internal/dto/` | 请求 / 响应传输对象，隔离领域模型与外部协议 | 无下游 |

跨层调用一律视为架构违规，CI 静态检查会拦截。

## 🤖 AI Agent 41 工具

ReAct 引擎内置 41 个原子工具，按域分组：

- **触达域**：短信 / 邮件 / WhatsApp / 微信群发 / 活码 / 短链
- **项目管理域**：营销画布、SOP 编排、AB 实验
- **客户域**：OneID 查询、客户 360°、RFM 分群、客户事件
- **知识库域**：RAG 检索、分块管理、知识库 CRUD
- **业务域**：订单 / 线索 / 售后 / 社群

完整工具清单见 [`../docs/architecture/agent-tools-inventory.md`](../docs/architecture/agent-tools-inventory.md)。

## 📁 目录结构

```
user-server/
├── cmd/
│   ├── api/                    # 主二进制 user-server 入口
│   ├── perf/                   # 性能压测工具
│   └── routeinspect/           # 路由自检工具（导出 ROUTES.md）
├── config/                     # 平台对接配置
├── docs/                       # Swagger 注释 + dev 文档
│   ├── AGENT_TOOLS.md
│   ├── swagger.go
│   └── dev/
│       ├── HOT_RELOAD.md        # ⭐ 热重载工作流（air 详解 + 边界约束）
│       ├── DEVELOPMENT.md
│       ├── ARCHITECTURE.md
│       ├── CONVENTIONS.md
│       └── FEATURES.md
├── internal/
│   ├── aiagent/                # 智能体引擎（ReAct / RAG / LLM 抽象）
│   ├── cache/                  # 缓存抽象（内存 + Redis）
│   ├── channelbot/             # 渠道机器人（Telegram / WhatsApp）
│   ├── config/                 # 业务配置
│   ├── content/                # AI 内容 / 素材 / 模板市场
│   ├── controller/             # HTTP 控制器（五层架构 · 第 1 层）
│   ├── cron/                   # 后台定时任务
│   ├── database/               # DB 与向量库抽象
│   ├── dto/                    # 传输对象（五层架构 · 第 5 层）
│   ├── email/                  # 邮件服务（草稿 / 列表 / 发送 / SMTP / 跟踪）
│   ├── etl/                    # 文档解析与处理
│   ├── event/                  # 事件总线与订阅者
│   ├── identity/               # 身份归一化（OneID）
│   ├── integration/            # 集成模板
│   ├── middleware/             # 中间件（鉴权 / 限流 / 审计 / 脱敏 / 追踪）
│   ├── migration/              # 数据库迁移
│   ├── model/                  # GORM 模型（五层架构 · 第 4 层）
│   ├── pkg/                    # 公共工具（i18n / metrics / trace / utils）
│   ├── platform/               # 与平台端通信（心跳 / 安装信息上报）
│   ├── repository/             # 数据访问层（五层架构 · 第 3 层）
│   ├── router/                 # 路由注册
│   ├── service/                # 业务编排（五层架构 · 第 2 层）
│   ├── template/               # HTML 模板
│   └── websocket/              # WebSocket 服务
├── config.yaml                 # 服务配置（dev 宿主直连；Docker 内经 ${ENV_VAR} 注入服务名寻址）
├── go.mod
├── go.sum
├── Dockerfile
└── .golangci.yml
```

## 🚀 启动说明

整体架构：**数据层（Docker）+ 宿主机推理栈（llama.cpp 三件套）+ user-server（air 热更新或二进制）**。

> ⭐ **日常开发只需一行 `make dev` 启动热重载**，完整工作流与边界约束见 [`docs/dev/HOT_RELOAD.md`](docs/dev/HOT_RELOAD.md)。本节为首次部署 / 全栈编排视角。

### 方式 A：宿主机推理栈 + 一键全栈（推荐）

仓库根 `hivemtk/` 已提供 Makefile 编排：

```bash
cd ../            # 回到 hivemtk 仓库根
make install      # 生成 .env + docker-compose.yml + 下载模型 + 拉起全栈
# 或分步：
make db-up                    # 启动 PG + Redis 容器
make inference-host-install   # 安装 llama.cpp（首次）
make inference-host-models    # 下载 dev 档模型（3B LLM + bge-m3 + bge-reranker-v2-m3）
make inference-host-up        # 启动三个 llama-server（LLM :8207 / Embedding :8208 / Rerank :8209）
make inference-host-warmup    # 预热三端点（避免首请求慢）
make dev                      # 启动 user-server（air 热更新）
```

切换 prod 档（14B LLM，需 32GB+ 内存）：

```bash
make inference-host-models-prod  # 下载 prod 档模型
HIVEMTK_PROFILE=prod make inference-host-up
```

启动后访问：

- 用户端 B 端工作台：`http://localhost:8211`（前端 `user-web`）
- API：`http://localhost:8204/api/v1/...`
- Swagger：`http://localhost:8204/swagger/index.html`
- 健康检查：`http://localhost:8204/health`
- LLM（OpenAI 兼容）：`http://127.0.0.1:8207/v1`
- Embedding（TEI 兼容）：`http://127.0.0.1:8208/v1`
- Rerank（TEI 兼容）：`http://127.0.0.1:8209`

### 方式 B：本地源码运行（开发态）

#### 前置要求

- Go 1.25+
- PostgreSQL 16（启用 pgvector 扩展）
- Redis 7+（可选，未配置时走进程内缓存）
- Node.js 20+（仅在需要构建前端时）

#### 步骤

```bash
# 1. 准备数据库
psql -U postgres -c "CREATE DATABASE user_db;"
psql -U postgres -d user_db -c "CREATE EXTENSION IF NOT EXISTS vector;"
psql -U postgres -d user_db -f ../migrations/init-user-db.sql
psql -U postgres -d user_db -f ../migrations/002_ai_content.sql
# 依次执行 ../migrations/ 下 002_*.sql ~ 031_*.sql

# 2. 配置环境变量
cp config.yaml config.local.yaml
# 修改 config.local.yaml 中的 postgres 密码、JWT_SECRET、PLATFORM_API_URL（平台端地址）
# （开发态用 air 启动时，数据库 / JWT / 推理等变量已由 .air.toml 的 [env] 内置，无需手动 export；
#   如需覆盖平台地址等，在启动前 export 同名变量或编辑 .air.toml 即可）

# 3. 拉取依赖
go mod download

# 4. 启动（开发态 · 热重载，⭐推荐，详见 docs/dev/HOT_RELOAD.md）
make dev-install   # 一次性安装 air（go install github.com/air-verse/air@latest）
make dev           # air 监听 .go/.yaml/.html/.env，自动重编+重启，零停机
# 仅生产 / 单次运行才手动编译二进制：
# go build -o bin/user-server ./cmd/api && ./bin/user-server
```

#### 验证

```bash
curl http://localhost:8204/health
# 期望：{"status":"ok","checks":{...}}

curl http://localhost:8204/api/v1/public/init
# 期望：返回初始化状态（已安装 / 未安装）
```

## ⚙️ 关键配置项

`config.yaml` 中重点关注：

| 配置块 | 说明 | 关键字段 |
|---|---|---|
| `database.postgres` | 数据库连接 | host / port / user / password / dbname / sslmode |
| `vector_database.pgvector` | 向量库 | table / dimension（默认 1024） |
| `inference.embedding` | Embedding 服务 | mode / base_url / model / dimension |
| `inference.rerank` | Rerank 服务 | mode / base_url / model / enabled |
| `inference.llm` | LLM 服务 | mode / base_url / model / temperature / max_tokens |
| `storage.qiniu` | 对象存储（七牛） | access_key / secret_key / bucket / domain |
| `chat.transfer_keywords` | 自动转人工关键词 | 命中后转人工排队 |
| `logging` | 日志 | level / format / output / file / max_size |

敏感字段（密码 / 密钥）一律通过 `${ENV_VAR}` 占位注入，禁止明文落盘。详见 [config.yaml](./config.yaml)。

## 🛠 常用命令

```bash
# === 开发热重载（⭐推荐，详见 docs/dev/HOT_RELOAD.md）===
make dev                           # 启动 user-server + air 热重载
make dev-install                   # 一次性安装 air（已装跳过）
make dev-stop                      # 停止 air
make dev-clean                     # 清理 air 临时产物
make dev-help                      # 热重载速查

# 生产构建
go build -o bin/user-server ./cmd/api

# 生产运行
./bin/user-server

# 测试（串行化，避免共享测试库并发 TRUNCATE 污染）
go test ./... -count=1 -timeout 300s -p 1

# 测试覆盖率
go test ./... -count=1 -p 1 -coverprofile=coverage.out -covermode=atomic
go tool cover -html=coverage.out -o coverage.html

# 静态检查
gofmt -w .
go vet ./...
golangci-lint run

# 路由自检（导出 ROUTES.md）
go run ./cmd/routeinspect -format=md > ROUTES.md

# 推理栈状态
make inference-host-status
```

## 🔌 主要 API 路由（节选）

| 路由 | 说明 |
|---|---|
| `POST /api/v1/auth/login` | 登录 |
| `POST /api/v1/auth/refresh` | 刷新 Token |
| `GET /api/v1/customer/list` | 客户列表 |
| `GET /api/v1/clue/list` | 线索列表 |
| `POST /api/chat/public/sessions/:session_id/messages` | 公开客服对话（AppKey 鉴权） |
| `GET /api/v1/knowledge/search` | 知识库检索 |
| `POST /api/v1/short-link/create` | 创建短链 |
| `GET /api/v1/dashboard/sse` | 看板实时数据（SSE） |
| `GET /health` `/healthz` `/readyz` | 健康检查 |
| `GET /swagger/index.html` | Swagger 文档 |

完整路由通过 `routeinspect` 工具导出：

```bash
go run ./cmd/routeinspect -format=md > ROUTES.md
```

## 🔐 私域合规基线

- 业务数据**仅本地存储**，无任何外发通道（除显式配置的微信 / 短信 / 邮件 / 对象存储回调）
- 数据库密码、JWT 密钥、对象存储密钥**全部**通过环境变量注入
- 完整审计日志（操作者 / 时间 / IP / 资源）持久化至 `audit_log` 表
- 推理栈全部跑在宿主机本地，模型权重本地存放，无外部 LLM API 调用

## 📚 关联文档

- **热重载工作流（⭐必读）**: [./docs/dev/HOT_RELOAD.md](./docs/dev/HOT_RELOAD.md)
- 仓库根 [README](../README.md)
- 架构图 [../docs/architecture/ARCHITECTURE_DIAGRAM.md](../docs/architecture/ARCHITECTURE_DIAGRAM.md)
- Go 五层架构规范 [../docs/architecture/GO_FIVE_LAYER_ARCHITECTURE.md](../docs/architecture/GO_FIVE_LAYER_ARCHITECTURE.md)
- AI Agent 41 工具清单 [../docs/architecture/agent-tools-inventory.md](../docs/architecture/agent-tools-inventory.md)
- 宿主机推理栈方案 [../docs/architecture/HOST_INFERENCE_PLAN.md](../docs/architecture/HOST_INFERENCE_PLAN.md)
- 商户部署手册 [../docs/operations/MERCHANT_DEPLOYMENT.md](../docs/operations/MERCHANT_DEPLOYMENT.md)

## 📄 许可证

本项目以 **GNU Affero General Public License v3.0（AGPL-3.0）** 发布，详见 [../LICENSE](../LICENSE) 与 [../NOTICE](../NOTICE)。

- 任何对本项目的修改与网络服务提供均须开源衍生代码（AGPL-3.0 第 13 条）
- 商业闭源集成 / 二次分发请先联系商务获取授权

商务合作 / 技术支持：jideilvluoqun@gmail.com

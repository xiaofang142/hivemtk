# user-server 代码开发手册

> **规则级别**: ⭐⭐ 项目级开发文档
> **关联文档**:
> - 架构图: [./ARCHITECTURE.md](./ARCHITECTURE.md)
> - 代码规范: [./CONVENTIONS.md](./CONVENTIONS.md)
> - 功能清单: [./FEATURES.md](./FEATURES.md)
> - **热重载工作流（⭐必读）**: [./HOT_RELOAD.md](./HOT_RELOAD.md)
> - 五层架构硬约束: [../../docs/architecture/GO_FIVE_LAYER_ARCHITECTURE.md](../../../docs/architecture/GO_FIVE_LAYER_ARCHITECTURE.md)
> - 工程 README: [../README.md](../../README.md)

本文档面向 `user-server` 工程的二次开发者，覆盖 **环境搭建、本地启动、目录导航、新增 API / AI 模块、数据库迁移、测试、调试、构建与部署** 八大主题。
所有命令默认在 `hivemtk/user-server/` 目录下执行（除非显式 `cd ..`）。

---

## 一、环境准备

### 1.1 必备依赖

| 依赖 | 最低版本 | 用途 | 校验命令 |
| --- | --- | --- | --- |
| Go | 1.25 | 主二进制编译 | `go version` |
| PostgreSQL | 16（必须启用 pgvector 扩展） | 业务主库 + 向量索引 | `psql --version` |
| Redis | 7（可选，未配置时回退进程内缓存） | 缓存 / 分布式锁 / 限流 | `redis-cli --version` |
| Node.js | 20（仅在构建前端 / embed-sdk 时需要） | 前端构建 | `node -v` |
| air | 最新 | Go 热重载（开发态推荐） | `go install github.com/air-verse/air@latest`（项目 Makefile `make dev-install` 包装） |
| llama.cpp | 最新（开发态可选） | 本地 LLM / Embedding / Rerank | `make inference-host-ps` |
| Docker | 24+（仅容器化部署场景） | 数据层 / 旧版全栈 | `docker --version` |

### 1.2 推荐工具链

- **IDE**: VS Code + Go 扩展（启用 `gopls`、`staticcheck`）
- **Linter**: `golangci-lint`（仓库已带 `.golangci.yml`，含 `depguard` 两条规则：`controller-layer` 禁止 controller 依赖 model/repository；`model-layer` 禁止 model 反向依赖 service/controller/repository）
- **DB 客户端**: DBeaver / pgAdmin（连接 PG 16）
- **API 调试**: curl / Postman / Bruno（**注意：Swagger 当前未注册**，`router.Setup()` 未挂载任何 gin-swagger 路由；如需开启，请在 `router.go` 中自行添加 `gin-swagger` 中间件并生成 swagger doc）

### 1.3 配置文件

| 文件 | 作用 | 是否入 Git |
| --- | --- | --- |
| `config.yaml` | 宿主直连配置（dev 默认） | ✅ 入仓（不含明文密钥） |
| `config.yaml` | Docker 内配置（服务名寻址） | ✅ 入仓 |
| `.air.toml` | air 热重载配置（已入仓，团队共享；本地覆盖请用 `.air.local.toml`） | ✅ 入仓 |
| `.env` | 敏感字段（POSTGRES_PASSWORD / JWT_SECRET 等） | ❌ 不入仓 |
| `config/platform.yaml` | 平台对接配置（api_url/secret/admin） | ✅ 入仓 |

敏感字段一律通过 `${ENV_VAR}` 占位符注入，**禁止**任何明文密钥写入受 Git 跟踪的文件。

---

## 二、本地启动

### 2.1 推荐：Docker 一键启动（首次部署）

```bash
cd ..                            # 回到 hivemtk 仓库根
make install                     # 生成 .env + docker-compose.yml + 拉模型 + 启 PG/Redis/推理栈
make dev                         # 启动 user-server 热更新（air）
cd user-web && npm run dev       # 启动前端（另一终端，已在 hivemtk/ 根目录下）
```

启动后访问：

- 用户端 B 端工作台: `http://localhost:8204`
- API: `http://localhost:8204/api/v1/...`
- Swagger: **当前未注册**（`router.Setup()` 未挂载 gin-swagger 路由；如需开启请自行在 `router.go` 添加）
- 健康检查: `http://localhost:8204/health` `/healthz` `/readyz`

### 2.2 纯源码启动（已有 PG/Redis/推理栈）

```bash
# 1. 准备数据库（首次）
psql -U postgres -c "CREATE DATABASE user_db;"
psql -U postgres -d user_db -c "CREATE EXTENSION IF NOT EXISTS vector;"
psql -U postgres -d user_db -f ../migrations/init-user-db.sql
# 后续 schema 由 main.go 启动时 migration.NewMigrationService 自动补齐

# 2. 复制配置
cp config.yaml config.local.yaml   # 仅本地修改，不入 Git

# 3. 拉依赖
go mod download

# 4. 启动（热重载，推荐，⭐详见 [./HOT_RELOAD.md](./HOT_RELOAD.md)）
air -c .air.toml

# 仅生产 / 单次运行才手动编译二进制：
# go build -o bin/user-server ./cmd/api && ./bin/user-server
```

### 2.3 验证启动成功

```bash
curl http://localhost:8204/health
# 期望：{"status":"ok","checks":{"postgres":"ok","redis":"not_configured",...}}

curl http://localhost:8204/api/v1/public/init
# 期望：返回初始化状态（installed: true/false）
```

### 2.4 端口约定

**全栈端口对照表**（所有调整必须先改本表 + `internal/config/ports.go` + bridge `constants.js`）：

| 端口 | 服务 / 应用 | 启动入口 | 单一源常量 | 文档源 |
| --- | --- | --- | --- | --- |
| 8202 | PostgreSQL（Docker 部署映射端口） | `docker compose -f docker-compose.yml up -d` | `config.DefaultDBPortDocker` | `docker-compose.yml` 中 mtk-postgres 容器映射宿主 8202 |
| 8203 | Redis | `docker compose -f docker-compose.yml up -d` | `config.DefaultRedisPort` | `docker-compose.yml` 中 mtk-redis 容器 |
| **8204** | **user-server**（Gin HTTP） | `go run ./cmd/api` 或 `air -c .air.toml` | `config.DefaultListenPort` / `main.DefaultListenPort` | `Dockerfile:57 ENV SERVER_PORT=8204` |
| 8205 | platform-server | `cd hivemtk-platform/platform-server && go run ./cmd/api` | `config.DefaultPlatformPort` | platform-server/config.yaml `server.port` |
| 8206 | Chromium CDP（远程调试） | `chromedp.Flag("remote-debugging-port", "8206")` | `config.DefaultChromiumCDPPort` | `internal/aiagent/agent/browser/assistant.go:43` |
| 8207 | LLM（llama.cpp） | `bash scripts/inference-host/start-llm.sh` | `config.DefaultLLMPort` | `inference.llm.base_url: http://127.0.0.1:8207/v1` |
| 8208 | Embedding（bge-m3） | `bash scripts/inference-host/start-embedding.sh` 或 `go run ./cmd/embedding-server` | `config.DefaultEmbeddingPort` | `inference.embedding.base_url: http://127.0.0.1:8208/v1` |
| 8209 | Rerank（bge-reranker-v2-m3） | `bash scripts/inference-host/start-rerank.sh` | `config.DefaultRerankPort` | `inference.rerank.base_url: http://127.0.0.1:8209/v1` |
| 8232 | PostgreSQL（dev 本机直连） | `pg_ctl -D /usr/local/var/postgres start` | `config.DefaultDBPortDev` | `config.yaml` 中 `database.postgres.port: 8232` |

**各应用启动描述**（按"开箱即用"顺序）：

```bash
# === 1. 启动数据层（PostgreSQL + Redis）===
# 端口 8202（PG）+ 8203（Redis）
cd hivemtk && make db-up
# 验证：
docker compose -f docker-compose.yml ps   # 两个容器都 healthy

# === 2. 启动本地推理栈（LLM + Embedding + Rerank）===
# 端口 8207 + 8208 + 8209（宿主机 llama.cpp）
cd hivemtk && make inference-host-up
# 首次需要：make inference-host-install  # 安装 llama.cpp
# 首次需要：make inference-host-models    # 下载 dev 档模型
# 验证：make inference-host-status

# === 3. 启动 user-server 主进程 ===
# 端口 8204
cd hivemtk/user-server
cp config.yaml.example config.yaml  # 首次；按 .env 调整 POSTGRES_PASSWORD
go run ./cmd/api
# 或热更新（⭐推荐，详见 [./HOT_RELOAD.md](./HOT_RELOAD.md)）：
cd hivemtk && make dev    # air 监听 ./user-server，保存 .go 即自动重启
# 验证：curl http://localhost:8204/health

# === 4. 启动 platform-server（仅当需要资产市场/超管后台）===
# 端口 8205
cd hivemtk/hivemtk-platform/platform-server
go run ./cmd/api
# 验证：curl http://localhost:8205/healthz

# === 5. 启动 user-web 前端 ===
# 默认 vite 5173
cd hivemtk/user-web
npm run dev
# 验证：浏览器打开 http://localhost:5173

# === 6. 启动 bridge 浏览器扩展（可选）===
# 端口 8204（连接 user-server）
cd hivemtk/user-web/bridge
node scripts/build.mjs
# Chrome → chrome://extensions → 加载已解压扩展 → 选择 dist/
# popup 端口默认 http://localhost:8204（user-server）
```

**各 cmd 入口的启动描述**（除主进程外，按需启动）：

| 入口 | 启动命令 | 端口 | 用途 | 单一源 |
| --- | --- | --- | --- | --- |
| `cmd/api` | `go run ./cmd/api` | 8204 | 主 HTTP 服务 | `config.DefaultListenPort` |
| `cmd/embedding-server` | `go run ./cmd/embedding-server -port=8208` | 8208 | 纯 Go Embedding HTTP（无 host 推理栈时备用） | `config.DefaultEmbeddingPort` |
| `cmd/seed` | `go run ./cmd/seed` 或 `go run ./cmd/seed --module=customers` | - | 演示种子数据写入（10 个模块） | - |
| `cmd/perf` | `go run ./cmd/perf -username=xxx -password=xxx` | - | 压测（必须显式传账号密码） | `config.DefaultUserServerBaseURL` |
| `cmd/routeinspect` | `go run ./cmd/routeinspect` | - | 路由自检（仅打印路由） | - |

**单一源约束（禁软启动 / 禁多处硬编码）**：

1. **所有端口字面量集中在 `user-server/internal/config/ports.go`**（`DefaultListenPort`/`DefaultDBPortDev`/`DefaultDBPortDocker`/`DefaultRedisPort`/`DefaultPlatformPort`/`DefaultChromiumCDPPort`/`DefaultLLMPort`/`DefaultEmbeddingPort`/`DefaultRerankPort` 等 + 对应 `_PortStr` 字符串版本 + `DefaultXxxBaseURLDev`/`DefaultXxxBaseURLDocker` 派生 URL + `DefaultPlatformAPI` 平台 API 网关 + `DefaultBGEBaseURLDev/Docker` BGE-m3 兜底）
2. **bridge 端单源**：`user-web/bridge/src/core/constants.js` 的 `DEFAULT_USER_SERVER.port = 8204`
3. **禁止"软启动"**——`config.yaml` 缺字段时必须明确报错（`log.Fatalf`），不允许 `if cfg == nil { cfg = defaultConfig }` 静默兜底；即使是最后兜底的本地默认值，也必须从 `config.DefaultXxxBaseURLDev` / `config.DefaultXxxBaseURLDocker` 引用，禁止字面量
4. **禁止"重复硬编码"**——任何模块（含 `cmd/perf/main.go`、`internal/service/short_link.go` 等）禁止在写 `http://localhost:8204` / `:8207` 等字面量，必须 `import "marketing/internal/pkg/utils/config"` 后用 `config.DefaultXxx` 派生
5. **fallback 必须可溯源**——embedding/rerank/llm 服务的 last-resort fallback（如 `embedding.go:163`、`rerank.go:137`、`llm.go:574`）必须引用 `config.DefaultXxxBaseURLDocker` 或 `config.DefaultXxxBaseURLDev`，禁止直接写 `http://mtk-xxx:8208/v1` 等字面量
6. **禁止账号/密码硬编码**——`cmd/perf/main.go` 等压测工具禁止 `admin123` 等弱口令默认值；必须通过命令行 `-username`/`-password` 或 `PERF_USERNAME`/`PERF_PASSWORD` 环境变量显式传入
7. **禁止模型名硬编码**——LLM/Embedding/Rerank 默认模型名集中通过 `config.DefaultLLMModel()` / `config.DefaultEmbeddingModel()` / `config.DefaultRerankModel()` getter 引用，禁止 `embedding.go:169` 类的 `model = "bge-m3"` 字面量（dev 档契约：`Qwen2.5-1.5B-Instruct` / `bge-m3` / `bge-reranker-v2-m3`）
8. **禁止外部 URL 硬编码**——平台 API 域名集中通过 `config.DefaultPlatformAPI` 引用，禁止 `cmd/api/main.go:175` 类字面量；Ollama 端口由环境变量 `PLAYGROUND_LLM_BASE_URL` 显式覆盖

**PostgreSQL 端口差异说明**：本地源码启动时 `config.yaml` 中端口为 **8232**；Docker 部署时 `docker-compose.yml` 把 mtk-postgres 容器的 5432 映射到宿主机 **8202**。两者不冲突——切换部署模式时需同步修改 `config.yaml` 或 `config.yaml` 的 `database.postgres.port`，或通过 `POSTGRES_PORT` 环境变量覆盖。

---

## 三、目录导航

```
user-server/
├── cmd/                                  L1 进程入口
│   ├── api/                              主二进制（main.go 装配 + 启动）
│   ├── embedding-server/                 可选 Embedding HTTP 服务（纯 Go char n-gram TF-IDF；Docker 不打包，host 推理栈不可用时单独构建）
│   ├── perf/                             压测工具
│   ├── routeinspect/                     路由自检工具（仅打印 chat-channels 路由到 stderr）
│   └── seed/                             种子数据工具（用户/客户/线索/AI Agent/资产等）
├── config/                               平台对接配置（platform.yaml）
├── docs/                                 dev 文档（Swagger 注释当前未启用）
│   ├── dev/HOT_RELOAD.md                 ⭐ 热重载工作流（air 详解 + 边界约束）
│   ├── dev/DEVELOPMENT.md               本文档（环境/启动/目录/新增API/迁移/测试/调试/构建）
│   ├── dev/ARCHITECTURE.md              架构图（模块 / 时序 / 子系统）
│   ├── dev/CONVENTIONS.md               代码规范（分层 / 命名 / 错误 / 日志）
│   └── dev/FEATURES.md                  功能清单（按业务域分组）
├── internal/
│   ├── router/                           L2 路由声明（router.go + *_routes.go）
│   ├── controller/                       L3 表现层（薄 handler）
│   ├── ops/controller · content/controller   L3 业务域独立子包
│   ├── service/                          L4 业务编排
│   ├── ops/service · content/service · email/service   L4 业务域独立子包
│   ├── service/{confidence,humanize,feedback_loop,i18n,self_learning,orderft}   L4 自闭子模块（避免循环依赖）
│   ├── repository/                       L5 数据访问（GORM + pgvector）
│   ├── model/                            横向 GORM 实体
│   ├── dto/                              横向 请求/响应
│   ├── middleware/                       横向 Gin 中间件（jwt/audit/trace/ratelimit/...）
│   ├── cache/                            横向 缓存抽象（memory + redis）
│   ├── event/                            横向 Event Bus + 订阅者
│   ├── websocket/                        横向 WS Hub（hub/handler/seq/ack_tracker/notify）
│   ├── platform/                         横向 平台对接 SDK（client/sync/adapter/heartbeat）
│   ├── migration/                        横向 迁移服务（registry/service/migrations）
│   ├── aiagent/                          能力层（agent/llm/rag/embedding/vector/eval/knowledge）
│   ├── integration/ · identity/ · etl/ · cron/ · domain/ · channelbot/ · config/   横向业务子包
│   └── pkg/                              通用工具（i18n/metrics/trace/testutil/utils）
├── config.yaml · config.yaml      宿主/Docker 配置
├── .air.toml                             air 热重载配置（不入仓，见 .gitignore）
├── .golangci.yml                         Linter 配置（含 depguard controller-layer / model-layer 两条规则）
├── Dockerfile                            多阶段构建
├── go.mod · go.sum
└── README.md
```

每个 `internal/<layer>/` 目录都是独立包，**严禁** `controller` 直接 import `repository` / `model`（详见 [CONVENTIONS.md §1](./CONVENTIONS.md)）。

---

## 四、新增 API 标准流程

以「新增客户标签 CRUD」为例，演示五层架构完整开发流。

### 4.1 步骤 1：定义 Model（`internal/model/customer_tag.go`）

```go
package model

import "time"

// CustomerTag 客户标签
type CustomerTag struct {
    ID        uint      `gorm:"primaryKey" json:"id"`
    Name      string    `gorm:"size:64;not null;uniqueIndex" json:"name"`
    Color     string    `gorm:"size:16" json:"color"`
    Status    int       `gorm:"default:1" json:"status"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}

func (CustomerTag) TableName() string { return "customer_tags" }
```

### 4.2 步骤 2：定义 DTO（`internal/dto/customer_tag.go`）

```go
package dto

import "marketing/internal/model"

// CreateCustomerTagRequest 创建请求
type CreateCustomerTagRequest struct {
    Name  string `json:"name" binding:"required"`
    Color string `json:"color"`
}

// ToModel 将 DTO 转 Model（转换逻辑放在 DTO 包，避免 Model 含业务方法）
func ToCustomerTagModel(req *CreateCustomerTagRequest) *model.CustomerTag {
    return &model.CustomerTag{
        Name:  req.Name,
        Color: req.Color,
    }
}

// CustomerTagResponse 响应
type CustomerTagResponse struct {
    ID    uint   `json:"id"`
    Name  string `json:"name"`
    Color string `json:"color"`
}
```

### 4.3 步骤 3：实现 Repository（`internal/repository/customer_tag.go`）

```go
package repository

import (
    "context"
    "marketing/internal/model"
    "marketing/internal/pkg/utils/db"
)

type CustomerTagRepository struct {
    base *BaseRepository
}

func NewCustomerTagRepository() *CustomerTagRepository {
    return &CustomerTagRepository{base: NewBaseRepository(db.GetDB())}
}

func (r *CustomerTagRepository) Create(ctx context.Context, t *model.CustomerTag) error {
    return r.base.db.WithContext(ctx).Create(t).Error
}

func (r *CustomerTagRepository) List(ctx context.Context) ([]model.CustomerTag, error) {
    var list []model.CustomerTag
    err := r.base.db.WithContext(ctx).Find(&list).Error
    return list, err
}
```

### 4.4 步骤 4：实现 Service（`internal/service/customer_tag.go`）

```go
package service

import (
    "context"
    "marketing/internal/dto"
    "marketing/internal/model"
    "marketing/internal/pkg/utils/logger"
    "marketing/internal/repository"
)

type CustomerTagService struct {
    repo *repository.CustomerTagRepository
}

func NewCustomerTagService() *CustomerTagService {
    return &CustomerTagService{repo: repository.NewCustomerTagRepository()}
}

func (s *CustomerTagService) Create(ctx context.Context, req *dto.CreateCustomerTagRequest) (*model.CustomerTag, error) {
    tag := dto.ToCustomerTagModel(req)
    if err := s.repo.Create(ctx, tag); err != nil {
        logger.ErrorfContext(ctx, "[CustomerTagService] create failed: %v", err)
        return nil, err
    }
    return tag, nil
}

func (s *CustomerTagService) List(ctx context.Context) ([]model.CustomerTag, error) {
    return s.repo.List(ctx)
}
```

### 4.5 步骤 5：实现 Controller（`internal/controller/customer_tag.go`）

```go
package controller

import (
    "net/http"
    "marketing/internal/dto"
    "marketing/internal/pkg/utils/response"
    "marketing/internal/service"
    "github.com/gin-gonic/gin"
)

type CustomerTagController struct {
    svc *service.CustomerTagService
}

func NewCustomerTagController() *CustomerTagController {
    return &CustomerTagController{svc: service.NewCustomerTagService()}
}

func (ctrl *CustomerTagController) Create(c *gin.Context) {
    var req dto.CreateCustomerTagRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        response.Error(c, http.StatusBadRequest, "参数错误", err.Error())
        return
    }
    tag, err := ctrl.svc.Create(c.Request.Context(), &req)
    if err != nil {
        response.Error(c, http.StatusBadRequest, "创建失败", err.Error())
        return
    }
    response.Success(c, tag, "创建成功")
}
```

### 4.6 步骤 6：注册路由（`internal/router/business_routes.go`）

```go
// 在 setupBusinessRoutes 函数内追加：
tagCtrl := controller.NewCustomerTagController()
g := r.Group("/api/v1/customer-tags")
{
    g.GET("", tagCtrl.List)
    g.POST("", tagCtrl.Create)
    // ... PUT / DELETE
}
```

### 4.7 步骤 7：自检 + 测试

```bash
# 架构合规检查（强制阻断违规）
bash ../../scripts/check-architecture.sh

# 编译
go build ./...

# 单测
go test ./internal/service/ -run CustomerTag -count=1 -v

# curl 验证
curl -X POST http://localhost:8204/api/v1/customer-tags \
     -H "Content-Type: application/json" \
     -d '{"name":"VIP","color":"#f00"}'
```

---

## 五、新增 AI 模块标准流程

`internal/aiagent/` 是 AI 能力层，被 Service 调用，**不调** 业务 Service（避免循环依赖）。新增 AI 模块需遵守：

### 5.1 目录结构

```
internal/aiagent/<new_module>/
├── <new_module>.go          主接口 + 实现
├── <new_module>_test.go     单元测试
└── README.md                模块说明（可选）
```

### 5.2 步骤

1. **定义接口**：在 `aiagent/<new_module>/<new_module>.go` 中定义 `Service` 接口，避免暴露内部结构。
2. **实现**：依赖通过构造函数注入（`db` / `cache` / `dispatcher`），**禁止**包级全局变量。
3. **接入 Service**：在 `internal/service/` 调用方中 `New<Module>Service(...)` 装配，不要在 controller 直接调 aiagent。
4. **接入 Dispatcher**：若涉及 LLM 调用，使用 `llm.GetGlobalDispatcher().Dispatch(ctx, scenario, prompt)`，并指定 `DispatchScenario`（7 个场景枚举之一：`intent_recognize` / `sop_reply` / `objection` / `friendly_chat` / `long_summary` / `high_quality` / `low_cost`）。
5. **接入 Event Bus**：若需异步通知（如知识库变更），通过 `event.GetGlobalBus().Publish(topic, payload)`，禁止同步阻塞主链路。
6. **接入 Trace Bus**：所有 LLM 调用必须带 `trace_id`（由 `middleware/trace.go` 注入到 `ctx`），`Dispatcher` 内部已自动上报 `llm_routing_logs`。

### 5.3 新增 Agent 工具

`internal/aiagent/agent/tooluse/` 下注册工具，遵循 `Agent Tool Inventory`（详见 [../../docs/marketing-features/agent-tools-inventory.md](../AGENT_TOOLS.md)）：

```go
// 1. 实现 Tool 接口
type MyTool struct{}
func (t *MyTool) Name() string { return "my_tool" }
func (t *MyTool) Description() string { return "..." }
func (t *MyTool) Execute(ctx context.Context, args map[string]any) (string, error) { ... }

// 2. 在 registerAllAgentTools(db) 中注册
toolRegistry.Register("my_tool", &MyTool{})
```

---

## 六、数据库迁移

### 6.1 迁移系统架构

- **Registry**: `internal/migration/registry.go` —— 维护 `map[version]Migration`
- **Service**: `internal/migration/service.go` —— 异步执行 + `WaitForTask` 同步等待
- **Migrations**: `internal/migration/migrations/*.go` —— 各版本迁移实现
- **注册入口**: `migrations.RegisterMigrations(registry, db)`（在 `initial_schema.go` 末尾）

`main.go` 启动时**同步等待**迁移完成（60s 超时），确保后续 `operation_logs` 审计表就绪。

### 6.2 新增迁移步骤

1. **创建文件**: `internal/migration/migrations/<feature>_migration.go`
2. **实现 `Migration` 接口**: `Version()` / `Name()` / `Description()` / `Up(ctx)` / `Down(ctx)`
3. **DDL 幂等**: 所有 `CREATE TABLE` / `CREATE INDEX` 必须带 `IF NOT EXISTS`
4. **注册**: 在 `RegisterMigrations` 末尾追加 `registry.Register(New<Feature>Migration(db))`
5. **测试**: 同目录创建 `<feature>_migration_test.go`，参考 `confidence_migration_test.go`

### 6.3 迁移模板

```go
package migrations

import (
    "context"
    "fmt"
    "marketing/internal/migration"
    "gorm.io/gorm"
)

type MyFeatureMigration struct{ db *gorm.DB }

func NewMyFeatureMigration(db *gorm.DB) *MyFeatureMigration { return &MyFeatureMigration{db: db} }

func (m *MyFeatureMigration) Version() string     { return "v2.9.0" }
func (m *MyFeatureMigration) Name() string        { return "我的新功能迁移" }
func (m *MyFeatureMigration) Description() string { return "创建 my_feature 表" }

func (m *MyFeatureMigration) Up(ctx context.Context) error {
    if m.db == nil { return fmt.Errorf("db is nil") }
    stmts := []string{
        `CREATE TABLE IF NOT EXISTS my_feature (
            id BIGSERIAL PRIMARY KEY,
            name VARCHAR(128) NOT NULL,
            created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
        )`,
        `CREATE INDEX IF NOT EXISTS idx_my_feature_name ON my_feature(name)`,
    }
    return execAll(ctx, m.db, stmts)
}

func (m *MyFeatureMigration) Down(ctx context.Context) error {
    return m.db.WithContext(ctx).Exec(`DROP TABLE IF EXISTS my_feature`).Error
}

var _ migration.Migration = (*MyFeatureMigration)(nil)
```

### 6.4 手动触发迁移

```bash
# 1. 启动时自动执行（推荐，main.go 已同步等待）
air -c .air.toml

# 2. 仅查看迁移任务状态
psql -U admin -d user_db -c "SELECT id, from_version, to_version, status, error_message FROM upgrade_tasks ORDER BY id DESC LIMIT 10;"
```

### 6.5 数据迁移要求（来自项目记忆）

- **不能**以"脏数据"为理由跳过数据
- 迁移前必须 `pg_dump` 备份，保留 30 天
- 双向回滚 SQL 必须提前生成
- `Down()` 必须保护业务数据（仅删表，不删有价值的历史数据）

---

## 七、测试

### 7.1 测试三层流程（来自项目记忆）

1. **第 1 轮**: curl 测试所有 API，修复接口级问题
2. **第 2 轮**: 检查所有页面和 API 交互（参数 / 路由 / 响应 / 字段）
3. **第 3 轮**: 打开页面，模拟多角色 UI 点击（Chrome 移动设备模拟）

### 7.2 单元测试

```bash
# 串行化运行（避免共享测试库并发 TRUNCATE 污染）
go test ./... -count=1 -timeout 300s -p 1

# 短模式（CI 友好，跳过依赖外部服务的长测试）
go test ./... -count=1 -timeout 300s -p 1 -short

# 单包测试
go test ./internal/service/ -run CustomerTag -count=1 -v

# 覆盖率
go test ./... -count=1 -p 1 -coverprofile=coverage.out -covermode=atomic
go tool cover -html=coverage.out -o coverage.html
```

### 7.3 测试约定

- 文件命名: `<source>_test.go`，与被测文件同包
- 测试函数: `Test<Function>_<Scenario>`（如 `TestCreate_AlreadyExists`）
- 共享测试库: 通过 `internal/pkg/testutil/testdb.go` 获取，**禁止**并发 TRUNCATE
- 测试脚本应能独立报告错误，作为回归测试 + 种子数据生成器
- 每个新接口至少 5 个测试用例，覆盖正常 + 边界 + 异常
- 必须 100% 修复，**不允许**任何跳过 / 异常处理

### 7.4 API 路由自检

```bash
# 仅打印 /api/chat-channels 路由到 stderr（无 -format 参数，不生成 ROUTES.md）
go run ./cmd/routeinspect

# 与前端 API 调用比对
bash ../../scripts/api-inventory.sh
# 产物: api-inventory.md（后端路由 vs 前端调用一致性报告）
```

---

## 八、调试技巧

### 8.1 日志

- **zerolog** 驱动，配置见 `config.yaml` 的 `logging:` 段
- `level: debug` 时输出全量 trace_id（开发态推荐）
- 文件日志路径: `logs/user-server.log`（`output: file` 或 `both` 时生效）

### 8.2 trace_id 全链路追踪

- `middleware/trace.go` 为每个请求生成 `trace_id`，注入 `ctx`
- Service / Repository / Dispatcher 调用必须 `ctx` 透传
- 日志通过 `logger.ErrorfContext(ctx, ...)` 自动附带 `trace_id`
- LLM 调用落库至 `llm_routing_logs` 表，含 `trace_id` / `provider` / `latency_ms` / `success`

### 8.3 调试接口

| 端点 | 用途 |
| --- | --- |
| `GET /health` | 全量健康检查（PG / Redis / LLM / Embedding / Rerank） |
| `GET /healthz` | 存活探针（K8s liveness） |
| `GET /readyz` | 就绪探针（K8s readiness） |

> ℹ️ **Swagger 当前未注册**：`router.Setup()` 未挂载 `gin-swagger` 路由，故无 `/swagger/*` 端点。如需开启请自行在 `router.go` 添加。

### 8.4 LLM 调用链路排查

```sql
-- 查看最近 10 次 LLM 调用（含 trace_id / provider / 延时 / 成功 / fallback）
SELECT id, trace_id, scenario, provider, model, model_type, vendor,
       latency_ms, success, is_fallback, from_cache, token_source,
       prompt_tokens, completion_tokens, total_cost, error_msg, created_at
FROM llm_routing_logs
ORDER BY id DESC LIMIT 10;

-- 查看降级率（按场景聚合）
SELECT scenario,
       COUNT(*) AS total,
       SUM(CASE WHEN is_fallback THEN 1 ELSE 0 END) AS fallback_count,
       ROUND(100.0 * SUM(CASE WHEN is_fallback THEN 1 ELSE 0 END) / COUNT(*), 2) AS fallback_rate
FROM llm_routing_logs
WHERE created_at > NOW() - INTERVAL '24 hours'
GROUP BY scenario;
```

### 8.5 Event Bus 排查

- 队列满时会丢弃新事件并日志告警，**不阻塞主流程**
- 关键路径（如 SalesEngine 主链路、订单创建）**不依赖** Event Bus，必须同步走 Service 编排
- 订阅者列表: `event.GetGlobalBus().Subscribers(topic)`（仅调试用）

### 8.6 常见坑

| 现象 | 原因 | 解决 |
| --- | --- | --- |
| `controller 直接引用 repository` | 违反五层架构 | 改走 `service` 中转 |
| `GORM Raw 中文关键词返回 0 行` | GORM 静默失败 | 改用 `LIKE '%' || ? || '%'` 或全文索引 |
| `images text[] 数组扫描失败` | GORM 不支持原生数组 | 用 `array_to_string(images, ',')` 转字符串 |
| `context deadline exceeded` | dispatcher 超时早于 LLM 推理 | 调大 `inference.llm.timeout_seconds` |
| `operation_logs 表永远空` | `InitGlobalDispatcherWithDB` 前未 `db.InitDB` | 检查 main.go 装配顺序 |
| `本地 LLM 永不命中` | `QualityScore` 低于 `MinQuality` 门槛 | 调高本地 provider 的 `QualityScore` |
| `云端 401 风暴` | 未配 api_key 却启用云端 | `cloud_providers` 中 `enabled: false` |

---

## 九、构建与部署

### 9.1 本地构建

```bash
# 主二进制
go build -o bin/user-server ./cmd/api

# Embedding 子服务（可选，仅当无 host 推理栈时使用）
go build -o bin/embedding-server ./cmd/embedding-server

# 多阶段 Docker 构建
docker build -t hivemtk/user-server:latest .
```

> ℹ️ **Embedding 子服务定位**：`cmd/embedding-server/` 源码仍保留（纯 Go char n-gram TF-IDF + 随机投影实现，无 Python/ONNX 依赖），供无 host 推理栈的环境单独构建运行。Docker 部署场景下，Embedding 能力由宿主机 llama.cpp / TEI 提供（详见 [../../docs/architecture/HOST_INFERENCE_PLAN.md](../../../docs/architecture/HOST_INFERENCE_PLAN.md)），故 user-server Docker 镜像**不打包** embedding-server 二进制。

### 9.2 Dockerfile 说明

- **阶段 1**: `golang:1.25-alpine` 仅编译 `user-server` 一个二进制（`./cmd/api/main.go`）；`cmd/embedding-server/` 源码保留但不打入镜像
- **阶段 2**: `alpine:3.19` 运行镜像，非 root 用户（`app:app`）运行
- **国内镜像源**: 阿里云 Alpine 镜像加速
- **Chromium**: 默认注释（线上演示不需要自动回复），需要时取消注释
- **配置**: `config.yaml` 复制为容器内 `config.yaml`
- **install.lock**: 不打进镜像（运行时由初始化流程写入，持久化到 `/app/data` 命名卷）

### 9.3 CI/CD（GitHub Actions）

配置文件: `../../.github/workflows/user-server-ci.yml`

**触发**: push / PR 作用于 `master` / `main` 分支（路径过滤: `user-server/**` / `user-web/**` / `embed-sdk/**`）

**Job 列表**:

| Job | 内容 | 阻断 |
| --- | --- | --- |
| `test` | 架构合规检查 + go vet + go build + go test -short | ✅ 阻断 |
| `user-web-lint` | npm ci + npm run build | ✅ 阻断 |
| `embed-sdk-lint` | npm ci + npm test | ✅ 阻断 |
| `api-inventory` | 生成 API 调用 / 路由一致性报告 | ❌ 非阻断（仅监控） |

**测试期环境变量**:

- `POSTGRES_HOST: 127.0.0.1`
- `POSTGRES_PORT: 5432`
- `POSTGRES_USER: admin`
- `POSTGRES_PASSWORD: password123`
- `POSTGRES_DB: marketing_tools`
- `PLATFORM_JWT_SECRET: ci-jwt-secret-placeholder-32-chars-min-len-OK`（≥32 字符）
- `EMBEDDING_ALLOW_FALLBACK: true`（允许 hash embedding 降级）
- `WECOM_DISABLE_OUTBOUND: 1`（禁用企微真实出站）

**PostgreSQL 服务**: `pgvector/pgvector:pg15`（含 pgvector 扩展）

### 9.4 部署检查清单

部署前确认：

- [ ] `go build ./...` 通过
- [ ] `bash scripts/check-architecture.sh` 通过
- [ ] `go vet ./...` 通过
- [ ] `go test ./... -count=1 -timeout 300s -p 1 -short` 通过
- [ ] `gofmt -l .` 输出为空
- [ ] `.env` 中 `POSTGRES_PASSWORD` / `JWT_SECRET` / `QINIU_ACCESS_KEY` 等已配置
- [ ] `config.yaml` 的 `inference.llm.timeout_seconds` 改为生产值（如 120s）
- [ ] 数据库已备份（`make db-backup`）
- [ ] 迁移已同步等待完成（检查 `upgrade_tasks` 表 status=completed）

### 9.5 私域合规基线

- 业务数据**仅本地存储**，无任何外发通道（除显式配置的渠道回调）
- 数据库密码 / JWT 密钥 / 对象存储密钥**全部**通过环境变量注入
- 完整审计日志（操作者 / 时间 / IP / 资源）持久化至 `operation_logs` 表（通过 `middleware/audit.go` 的 `auditLogChan` 异步落库，**不经过 Event Bus**）
- 本地推理栈默认（`inference.llm.mode=local` / `inference.embedding.mode=local`），数据不出域
- 云端 LLM 仅在显式配置 `api_key` 且 `enabled=true` 时启用，作为可选 fallback

---

## 十、常用命令速查

```bash
# === 开发 ===
make dev                           # 启动 user-server 热更新（air，⭐详见 ./HOT_RELOAD.md）
make dev-stop                      # 停止 air 进程
make dev-clean                     # 清理 user-server/tmp/（air 临时二进制 + 日志）
make dev-help                      # 打印热重载工作流速查
make dev-all                       # 一键全栈（数据层 + 推理栈 + air 提示）
make dev-down                      # 停止全栈

# === 数据层 ===
make db-up                         # 启动 PG + Redis
make db-down                       # 停止 PG + Redis
make db-ps                         # 查看容器状态
make db-logs                       # 查看容器日志
make db-backup                     # 备份 PG
make db-restore FILE=backup_xxx.sql  # 恢复 PG

# === 推理栈（宿主机 llama.cpp）===
make inference-host-install        # 安装 llama.cpp（首次）
make inference-host-models         # 下载 dev 档模型
make inference-host-up             # 启动 LLM + Embedding + Rerank
make inference-host-down           # 停止三服务
make inference-host-warmup         # 预热（避免首请求慢）
make inference-host-test           # 端到端 smoke test
make inference-host-status         # 统一查看数据层 + 推理栈 + user-server 状态

# === 前端 ===
make web-build                     # 构建 user-web
make sdk-build                     # 构建 embed-sdk

# === Go ===
go build ./...                     # 全量编译
go vet ./...                       # 静态检查
gofmt -w .                         # 格式化
golangci-lint run                  # Linter
go test ./... -count=1 -p 1 -short  # 单测（短模式）

# === 工具 ===
go run ./cmd/routeinspect                          # 打印 /api/chat-channels 路由到 stderr（无 -format 参数）
bash ../../scripts/check-architecture.sh           # 架构合规检查
bash ../../scripts/api-inventory.sh                # API 一致性报告
```

---

## 十一、相关文档导航

| 主题 | 文档路径 |
|---|---|
| **热重载工作流（air 详解）** | [./HOT_RELOAD.md](./HOT_RELOAD.md) |
| 架构图（模块 / 时序 / 子系统） | [./ARCHITECTURE.md](./ARCHITECTURE.md) |
| 代码规范（分层约束 / 命名 / 错误处理 / 日志） | [./CONVENTIONS.md](./CONVENTIONS.md) |
| 功能清单（按业务域分组） | [./FEATURES.md](./FEATURES.md) |
| 五层架构硬约束 + 编码模板 | [../../docs/architecture/GO_FIVE_LAYER_ARCHITECTURE.md](../../../docs/architecture/GO_FIVE_LAYER_ARCHITECTURE.md) |
| 系统级 C4 / Container / Deployment | [../../docs/architecture/ARCHITECTURE_DIAGRAM.md](../../../docs/architecture/ARCHITECTURE_DIAGRAM.md) |
| 用户/角色/授权三模块 | [../../docs/architecture/USER_SYSTEM.md](../../../docs/architecture/USER_SYSTEM.md) |
| 菜单与权限设计 | [../../docs/architecture/MENU_PERMISSION_PLAN.md](../../../docs/architecture/MENU_PERMISSION_PLAN.md) |
| 营销功能模块索引（94+ 子模块） | [../../docs/marketing-features/README.md](../../../docs/marketing-features/README.md) |
| host 推理栈部署方案 | [../../docs/architecture/HOST_INFERENCE_PLAN.md](../../../docs/architecture/HOST_INFERENCE_PLAN.md) |
| 工程级 README | [../README.md](../../README.md) |
| 函数清单 | `user-server/NEW_FUNCTIONS_INVENTORY.md`（机器生成，.gitignore 排除，不入库） |

---

最近更新日期: 2026-07-26

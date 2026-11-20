# HiveMtk 代码导航与历史目录清单

> 本仓库为 **HiveMtk 用户端**(商户私有化部署部分)。平台端(`hivemtk-platform`)是独立仓库,不在本仓库内。

## 当前维护导航（2026-11-20 复核）

下方“一”至后续章节保留 2026-09-04、master `86575d6` 的目录快照；文件数量、页面数量和当时路径不作为当前契约。当前规则以根 `AGENTS.md`、`docs/standards/` 与实际构建入口为准，跨仓职责见外层仓 `docs/architecture/ARCHITECTURE_OVERVIEW.md`。

| 维护事项 | 当前入口与职责 |
|---|---|
| 启动与装配 | `user-server/cmd/api/main.go` 启停资源；`internal/app/` 组装运行时；`internal/router/` 映射路由并允许 DI；`internal/controller/` 处理请求和响应 |
| 五层与域目录 | 根 Controller / Service / Repository / Model 与 `internal/geo/`、`internal/content/` 等域目录并存；`scripts/check-layer-boundary.py` 按实际层名识别嵌套导入，结合 shell 门和 depguard 检查 |
| 菜单与功能分组 | `user-server/internal/model/menu.go` 的 `SystemMenuTree`；`user-web/src/router/modules/` 和懒加载别名；业务对象与菜单组不要求目录一一对应 |
| 共享 JavaScript | `packages/http-core/` 由管理端 request 与桥 SSE 消费；`packages/browser-core/` 由桥和浏览器自动化消费。共享包变更触发 SDK、管理端、桥及自动化消费测试 |
| 两套扩展工程 | `user-web/bridge/` 为渠道桥；`user-web/browser_automation/` 为浏览器自动化，分别有依赖锁、测试与构建入口；两者不是重复目录 |
| 数据演进 | 根 `migrations/` 为 SQL；`user-server/internal/migration/` 为 Go 注册式迁移，两者均须按实际入口核对，不能仅凭名称认为重复 |
| 定时任务生命周期 | `internal/pkg/cron/` 的 TaskManager 管理调度、真实任务体防重入与关闭等待；其他调度器仍由各自启动处负责停止 |
| 发布与部署 | `deploy/deploy-api.sh` 为 API 链 A；`docs/operations/RELEASE-PIPELINE.md` 说明扩展和静态资源发布，生产 SPA 由 nginx 托管 |
| 相邻独立仓 | 外层工作区另有 `hivemtk-platform/` 控制平面与 `assetdpo/` 资产工厂，各自提交、测试与发布；不属于本仓 submodule |

## 历史快照（2026-09-04，master 86575d6）

## 一、顶层结构

```
hivemtk/
├── user-server/          # Go 后端(Gin+GORM+PostgreSQL+Redis+pgvector)——核心,88MB,11.2万行
├── user-web/             # Vue 3 前端(Vite+Element Plus+Pinia),8.5万行 JS/Vue
├── embed-sdk/            # 嵌入式客服聊天浮窗 SDK(原生 JS+Vite)
├── migrations/           # SQL 迁移脚本(50 个)+ init-user-db.sql(建库/建扩展)
├── scripts/              # 运维/CI/推理栈脚本(check-architecture.sh、inference-host/ 等)
├── tests/                # 回归测试脚本(asset_market / permission API)
├── docs/                 # 部署指南、端口注册表、架构文档、排障手册
├── deploy/               # 部署物料
├── docker-compose.yml    # 仅数据层:PG15+pgvector(8202) + Redis(8203)
├── Makefile              # 全部构建/启动入口(make install / dev / test-go / lint)
├── .env-example          # 环境变量样例(端口/密钥/模型)
├── CONTRIBUTING.md       # 贡献指南
├── docs/contribute/GIT_RULES.md          # Git 分支与 commit 规范(唯一权威)
├── CLAUDE.md             # AI 编码铁律(五层架构/API 规范)
├── CLA.md / LICENSE      # 贡献者协议 / AGPL-3.0
└── hivemtk/scripts/      # 子目录仅含脚本(非重复代码)
```

## 二、user-server(Go 后端)

### 入口 `cmd/`
| 目录 | 用途 |
|------|------|
| `cmd/api/` | **主服务**(8204 端口,main.go 装配 DI+cron+迁移) |
| `cmd/bridge-mock/` | 渠道桥接模拟器(本地调试) |
| `cmd/embedding-server/` | 独立 embedding 服务 |
| `cmd/geo-run/`、`cmd/eval-trace/`、`cmd/perf/`、`cmd/routeinspect/` | GEO/评测/压测/路由巡检工具 |
| `cmd/importfaq/`、`cmd/syncscripts/` | FAQ 导入、脚本同步工具 |

### 内部五层架构 `internal/`(共 38 个包)
```
Router → Controller(Handler) → Service → Repository → Model
CI 通过 scripts/check-architecture.sh 强制检查,禁止跨层调用
```

| 层 | 目录 | 规模(非测试文件) |
|----|------|------|
| Router | `router/` | 27 个路由文件(按域拆分:auth/business/system/admin/geo/embed_static...) |
| Controller | `controller/` | 210 个文件 |
| Service | `service/` | 289 个文件(501 个含测试) |
| Repository | `repository/` | 201 个文件 |
| Model | `model/` | 174 个文件 |

### service 层主要业务域(60+,列举代表性)
- **渠道触达**:bridge(桥接入站)、chat_channel、wecom/dingtalk/feishu/telegram/whatsapp/email/sms 各渠道
- **AI 核心**:ai_agent(ReAct 智能体)、ai_debounce、intent_recognition、humanize(拟人化)、confidence
- **客户运营**:customer_360、customer_journey、user_segment(RFM)、clue(线索)、churn_score(流失)
- **营销资产**:card(抖音/快手/小红书/闲鱼卡片)、short_link、script_template、asset_bundle/asset_market
- **知识/RAG**:knowledge、faq、rag、geo(GEO 优化)
- **SOP 自动化**:sop_agent、sop_template、workflow_orchestrator、ab_experiment
- **平台协作**:platform(心跳/授权)、asset_market(资产市场)、alert_*(告警)

### 其他关键包
| 包 | 用途 |
|----|------|
| `config/` | 配置加载,`ports.go` 是端口唯一代码源(8204/8202/8203/8207-8209) |
| `pkg/db/` | GORM 连接 + AutoMigrate(自动建 pgvector 扩展,白名单校验) |
| `aiagent/` | 智能体运行时、LLM dispatcher(含 failover/trace/cache) |
| `migration/` | 启动期迁移框架(注册制,`migrations/` 下 30+ 个迁移器) |
| `secrets/` | MASTER_KEY AEAD 字段加密(release 模式缺失则拒绝启动) |
| `security/` | NetworkExposureGuard 私网护栏 |
| `websocket/`、`event/`、`cron/` | WS hub、事件总线、定时任务 |

## 三、user-web(Vue 3 前端)

```
user-web/src/
├── api/            # API 出口(统一封装)
├── views/          # 80 个视图目录,180 个 .vue(与后端业务域一一对应)
│   ├── 渠道:chat/ bridge/ email/ sms/ telegram/ wecomAccount/ dingtalkApp/
│   │        feishu/ whatsapp/ whatsappCloud/ whatsappBot/ xiaohongshuCard/
│   │        douyinCard/ kuaishouCard/ tiktokCard/ xianyuCard/
│   ├── AI:aiAgent/ intentRecognition/ humanize/ confidence/ sopAgent/
│   ├── 客户:customer360/ customerJourney/ userSegment/ tagSegmentation/
│   ├── 营销:reachPipeline/ marketingFlow/ abExperiment/ leadMining/
│   ├── 系统:system/ setup/ llmRouting/ backup/ securityAudit/
│   └── 其他:knowledge/ knowledgeBase/ geo/ assetMarket/ livecode/ ...
├── components/     # 24 个公共组件(PageHeader/PageState/QuickReplyPanel...)
├── stores/         # Pinia 状态
├── router/、layout/、i18n/、styles/、utils/、composables/、stories/(Storybook)
└── 约束:纯 JavaScript,禁止 TypeScript
```

## 四、embed-sdk(嵌入式 SDK)

原生 JavaScript + Vite,构建为 IIFE 产物 `marketing-chat-widget.iife.js`,由 user-server 托管在 `/embed/*`,第三方网站通过 `<script>` 引入即得客服聊天浮窗。

## 五、数据层与推理栈

| 组件 | 端口 | 说明 |
|------|------|------|
| PostgreSQL 15 + pgvector | 8202(Docker)/ 8232(原生 dev) | 1024 维 HNSW 向量索引,knowledge_embeddings 等 300+ 表(AutoMigrate 自动建) |
| Redis 7 | 8203 | 幂等守卫/缓存/限流 |
| llama.cpp LLM | 8207 | Qwen2.5-3B(Q4_K_M,2GB),支持 ReAct function-call |
| Embedding | 8208 | bge-m3(1024 维) |
| Rerank | 8209 | bge-reranker-v2-m3 |

`scripts/inference-host/` 提供 install/download/start/warmup/smoke-test 全套脚本;LLM 提供商配置运行时存 `llm_providers` 表(后台「LLM 路由」页管理),不落配置文件。

## 六、启动顺序(官方)

```
cp .env-example .env(改密钥)
→ make install(前端构建+模型下载+数据层+推理栈)
→ make dev(air 热更新启动 user-server)
→ cd user-web && npm run dev(前端 8211)
→ 健康检查 curl http://localhost:8204/health
→ 首次访问调 POST /api/system/init-admin 创建超管
```

## 七、质量护栏

| 工具 | 命令 | 作用 |
|------|------|------|
| check-architecture.sh | CI | 五层架构静态检查(禁止跨层调用) |
| golangci-lint + depguard | `make lint` | 分层依赖方向护栏(`user-server/.golangci.yml`) |
| go vet / go test | `make vet` / `make test-go` | 静态检查+单测 |
| Playwright | `tests/ui/user/` | 前端 UI 测试 |
| smoke-test.sh | `make inference-host-test` | 推理栈端到端 |
| commit-msg 钩子 | `git config core.hooksPath .githooks` | Conventional Commits 校验 |
